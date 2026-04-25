package cost

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Tally aggregates token usage across an entire run. EngineUsage keys are
// engine names ("claude", "codex"); within each entry tokens are split
// input vs. output where the underlying response shape made that
// distinction available, otherwise the total lands on Output (a
// pessimistic default — output rates are higher).
type Tally struct {
	EngineUsage map[string]Usage
}

// Usage is one engine's bookkeeping for a run.
type Usage struct {
	Calls        int
	InputTokens  int
	OutputTokens int
}

// TotalTokens reports the unsegmented total for callers that only care
// about the gross count (e.g. budget enforcement, which historically did
// not split input vs. output).
func (u Usage) TotalTokens() int { return u.InputTokens + u.OutputTokens }

// Add merges another tally into this one in place.
func (t *Tally) Add(engine string, u Usage) {
	if t.EngineUsage == nil {
		t.EngineUsage = map[string]Usage{}
	}
	cur := t.EngineUsage[engine]
	cur.Calls += u.Calls
	cur.InputTokens += u.InputTokens
	cur.OutputTokens += u.OutputTokens
	t.EngineUsage[engine] = cur
}

// EstimateUSD applies DefaultPricing across the tally and returns the
// total dollar estimate for the run.
func (t Tally) EstimateUSD() float64 {
	var total float64
	for engine, u := range t.EngineUsage {
		p := PricingFor(engine)
		total += p.EstimateUSD(u.InputTokens, u.OutputTokens)
	}
	return total
}

// FromRunDir walks a single run directory and tallies token usage by
// inspecting the per-round response artifacts AIOS already persists.
//
// Detection heuristics:
//   - coder.response.raw / reviewer.response.raw lines that parse as JSON
//     and carry a usage field are summed.
//   - The *.prompt.txt sibling indicates whether the call was a coder or
//     reviewer call, but for cost purposes we only care about which
//     engine ran — that decision lives in the project's config, but we
//     can usually infer it from the JSON shape (Claude has
//     usage.input_tokens; Codex's NDJSON has type=usage events).
//
// Anything we cannot classify is silently skipped; a near-empty tally is
// better than a crash on a malformed log line.
func FromRunDir(runRoot string) (Tally, error) {
	t := Tally{}
	// Distinguish "no audit found" from "empty audit": the former is a
	// configuration mistake (wrong path), the latter is a real run that
	// just had nothing parseable.
	if _, err := os.Stat(runRoot); err != nil {
		return t, err
	}
	err := filepath.WalkDir(runRoot, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // tolerate per-file errors
		}
		base := filepath.Base(path)
		if base != "coder.response.raw" && base != "reviewer.response.raw" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		engine, u := classify(raw)
		if engine == "" {
			return nil
		}
		t.Add(engine, u)
		return nil
	})
	return t, err
}

// classify inspects a single raw response payload and returns the engine
// name plus the usage extracted from it. Empty engine means "could not
// classify" — the caller should ignore the file.
func classify(raw []byte) (string, Usage) {
	trim := strings.TrimSpace(string(raw))
	if trim == "" {
		return "", Usage{}
	}
	// Try Claude single-object JSON shape first.
	if claudeEng, u, ok := tryClaudeJSON(raw); ok {
		return claudeEng, u
	}
	// Try Codex NDJSON shape (sums every "usage" event).
	if codexEng, u, ok := tryCodexNDJSON(raw); ok {
		return codexEng, u
	}
	// Try Codex single-object shape.
	if codexEng, u, ok := tryCodexSingleJSON(raw); ok {
		return codexEng, u
	}
	return "", Usage{}
}

// tryClaudeJSON only matches Claude's output shape. The discriminator is
// `type == "result"` (Claude's top-level wrapper) plus the input/output
// pair — both fields. Codex single-object output uses `total_tokens` as
// its only usage field, so that shape is rejected here and falls through
// to tryCodexSingleJSON. Without this guard a Codex response with
// per-call input/output token splits gets priced at Claude rates.
func tryClaudeJSON(raw []byte) (string, Usage, bool) {
	var doc struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", Usage{}, false
	}
	if doc.Type != "result" {
		return "", Usage{}, false
	}
	if doc.Usage.InputTokens == 0 && doc.Usage.OutputTokens == 0 {
		return "", Usage{}, false
	}
	return "claude", Usage{Calls: 1, InputTokens: doc.Usage.InputTokens, OutputTokens: doc.Usage.OutputTokens}, true
}

func tryCodexSingleJSON(raw []byte) (string, Usage, bool) {
	var doc struct {
		Type  string `json:"type"`
		Usage struct {
			TotalTokens  int `json:"total_tokens"`
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", Usage{}, false
	}
	in, out := doc.Usage.InputTokens, doc.Usage.OutputTokens
	if in == 0 && out == 0 && doc.Usage.TotalTokens == 0 {
		return "", Usage{}, false
	}
	if in == 0 && out == 0 {
		// Pessimistic split: weight all of total to output (higher rate).
		out = doc.Usage.TotalTokens
	}
	return "codex", Usage{Calls: 1, InputTokens: in, OutputTokens: out}, true
}

func tryCodexNDJSON(raw []byte) (string, Usage, bool) {
	lines := strings.Split(string(raw), "\n")
	// Don't gate on line count — a single valid {"type":"usage",...} event
	// is enough to identify the format. The `any` flag below is the real
	// gate: at least one usage event must be present for us to claim NDJSON.
	var u Usage
	any := false
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev struct {
			Type         string `json:"type"`
			InputTokens  int    `json:"input_tokens"`
			OutputTokens int    `json:"output_tokens"`
		}
		if err := json.Unmarshal([]byte(ln), &ev); err != nil {
			return "", Usage{}, false // not NDJSON
		}
		if ev.Type == "usage" {
			u.InputTokens += ev.InputTokens
			u.OutputTokens += ev.OutputTokens
			any = true
		}
	}
	if !any {
		return "", Usage{}, false
	}
	u.Calls = 1
	return "codex", u, true
}

// Render writes a human-readable cost summary to w. Sample output:
//
//	cost summary
//	─────────────────────────────────────────────
//	claude   42 calls   132,500 in    87,200 out   $8.52
//	codex    18 calls    44,100 in    23,800 out   $0.70
//	─────────────────────────────────────────────
//	total                                           $9.22
func (t Tally) Render(w io.Writer) {
	bar := strings.Repeat("─", 60)
	fmt.Fprintln(w, "cost summary")
	fmt.Fprintln(w, bar)
	if len(t.EngineUsage) == 0 {
		fmt.Fprintln(w, "no token usage detected — was this a dry-run?")
		fmt.Fprintln(w, bar)
		return
	}
	engines := make([]string, 0, len(t.EngineUsage))
	for k := range t.EngineUsage {
		engines = append(engines, k)
	}
	sort.Strings(engines)
	var total float64
	for _, e := range engines {
		u := t.EngineUsage[e]
		p := PricingFor(e)
		usd := p.EstimateUSD(u.InputTokens, u.OutputTokens)
		total += usd
		fmt.Fprintf(w, "%-8s %4d calls  %9d in  %9d out  $%6.2f\n",
			e, u.Calls, u.InputTokens, u.OutputTokens, usd)
	}
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "total%55s$%6.2f\n", "", total)
}
