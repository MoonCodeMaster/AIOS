package cost

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPricingFor_KnownAndUnknown(t *testing.T) {
	if got := PricingFor("claude"); got.Model == "" {
		t.Errorf("PricingFor(claude) returned zero-value")
	}
	if got := PricingFor("not-a-real-engine"); got.Model == "" {
		t.Errorf("PricingFor(unknown) should return placeholder, got zero-value")
	}
	if got := PricingFor("not-a-real-engine"); got.InputUSDPerMTok != 0 || got.OutputUSDPerMTok != 0 {
		t.Errorf("unknown pricing should have zero rates, got %+v", got)
	}
}

func TestEstimateUSD_KnownPricing(t *testing.T) {
	// claude opus: $15 in, $75 out per million tokens.
	p := PricingFor("claude")
	got := p.EstimateUSD(1_000_000, 1_000_000)
	want := 90.0 // 15 + 75
	if got != want {
		t.Errorf("EstimateUSD = %v, want %v", got, want)
	}
	got = p.EstimateUSD(0, 0)
	if got != 0 {
		t.Errorf("zero tokens = %v, want 0", got)
	}
}

func TestTally_AddAccumulates(t *testing.T) {
	var tly Tally
	tly.Add("claude", Usage{Calls: 1, InputTokens: 100, OutputTokens: 50})
	tly.Add("claude", Usage{Calls: 2, InputTokens: 200, OutputTokens: 100})
	got := tly.EngineUsage["claude"]
	if got.Calls != 3 || got.InputTokens != 300 || got.OutputTokens != 150 {
		t.Errorf("Tally.Add merged wrong: %+v", got)
	}
}

func TestClassify_ClaudeJSON(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":1234,"output_tokens":567}}`)
	eng, u := classify(raw)
	if eng != "claude" {
		t.Errorf("engine = %q, want claude", eng)
	}
	if u.InputTokens != 1234 || u.OutputTokens != 567 || u.Calls != 1 {
		t.Errorf("usage = %+v", u)
	}
}

func TestClassify_CodexSingleJSON(t *testing.T) {
	raw := []byte(`{"type":"response","text":"ok","usage":{"total_tokens":2000}}`)
	eng, u := classify(raw)
	if eng != "codex" {
		t.Errorf("engine = %q, want codex", eng)
	}
	if u.OutputTokens != 2000 {
		t.Errorf("output tokens = %d, want 2000 (total assigned to output)", u.OutputTokens)
	}
}

func TestClassify_CodexNDJSON(t *testing.T) {
	raw := []byte(`{"type":"response","content":"hi"}
{"type":"usage","input_tokens":300,"output_tokens":150}
{"type":"usage","input_tokens":50,"output_tokens":25}
`)
	eng, u := classify(raw)
	if eng != "codex" {
		t.Errorf("engine = %q, want codex", eng)
	}
	if u.InputTokens != 350 || u.OutputTokens != 175 {
		t.Errorf("usage = %+v; want in=350 out=175", u)
	}
}

// Regression: a Codex single-object response that happens to populate
// input_tokens / output_tokens (instead of total_tokens) must NOT be
// classified as Claude — Claude pricing is 3-15x higher and the silent
// mis-attribution would inflate the cost report by a wide margin.
func TestClassify_CodexLooksLikeClaude_StillCodex(t *testing.T) {
	raw := []byte(`{"type":"response","text":"ok","usage":{"input_tokens":1000,"output_tokens":500}}`)
	eng, _ := classify(raw)
	if eng == "claude" {
		t.Errorf("Codex response with input_tokens/output_tokens was misclassified as claude — would price at Claude rates")
	}
}

func TestFromRunDir_MissingDirReturnsErr(t *testing.T) {
	_, err := FromRunDir("/this/path/does/not/exist/anywhere")
	if err == nil {
		t.Error("FromRunDir should error on missing root, not silently return empty tally")
	}
}

func TestClassify_UnknownReturnsEmpty(t *testing.T) {
	if eng, _ := classify([]byte("not json")); eng != "" {
		t.Errorf("unknown classified as %q", eng)
	}
	if eng, _ := classify([]byte("{}")); eng != "" {
		t.Errorf("empty JSON classified as %q", eng)
	}
}

func TestFromRunDir_AggregatesAcrossRounds(t *testing.T) {
	// Build a fake run dir matching AIOS's on-disk layout:
	//   <tmp>/runs/<id>/<task>/round-N/coder.response.raw
	tmp := t.TempDir()
	mk := func(rel string, body string) {
		full := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("task-001/round-1/coder.response.raw",
		`{"type":"result","usage":{"input_tokens":1000,"output_tokens":500}}`)
	mk("task-001/round-1/reviewer.response.raw",
		`{"type":"response","text":"ok","usage":{"total_tokens":800}}`)
	mk("task-002/round-1/coder.response.raw",
		`{"type":"result","usage":{"input_tokens":2000,"output_tokens":1000}}`)
	mk("task-002/round-1/coder.prompt.txt", "ignored — not a response file")

	tly, err := FromRunDir(tmp)
	if err != nil {
		t.Fatalf("FromRunDir: %v", err)
	}
	cl := tly.EngineUsage["claude"]
	cx := tly.EngineUsage["codex"]
	if cl.InputTokens != 3000 || cl.OutputTokens != 1500 {
		t.Errorf("claude usage wrong: %+v", cl)
	}
	if cx.OutputTokens != 800 {
		t.Errorf("codex usage wrong: %+v", cx)
	}
	if usd := tly.EstimateUSD(); usd <= 0 {
		t.Errorf("EstimateUSD should be > 0, got %v", usd)
	}
}

func TestRender_EmptyTallyExplains(t *testing.T) {
	var buf bytes.Buffer
	(Tally{}).Render(&buf)
	if !strings.Contains(buf.String(), "no token usage detected") {
		t.Errorf("empty render missing explainer:\n%s", buf.String())
	}
}

func TestRender_TotalAppearsLast(t *testing.T) {
	var tly Tally
	tly.Add("claude", Usage{Calls: 1, InputTokens: 1000, OutputTokens: 500})
	var buf bytes.Buffer
	tly.Render(&buf)
	out := buf.String()
	if !strings.Contains(out, "total") || !strings.Contains(out, "$") {
		t.Errorf("render missing total / dollar sign:\n%s", out)
	}
}

// Codex 0.125+ uses turn.completed with nested usage, not flat usage events.
// Without this case the cost tallier ignores codex 0.125 calls entirely.
func TestTryCodexNDJSON_v0_125(t *testing.T) {
	raw := []byte(`{"type":"thread.started","thread_id":"x"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}
{"type":"turn.completed","usage":{"input_tokens":1234,"output_tokens":56,"reasoning_output_tokens":7}}
`)
	eng, u := classify(raw)
	if eng != "codex" {
		t.Fatalf("engine = %q, want codex", eng)
	}
	if u.InputTokens != 1234 {
		t.Errorf("InputTokens = %d, want 1234", u.InputTokens)
	}
	// reasoning is billable, fold into output for pricing.
	if u.OutputTokens != 63 {
		t.Errorf("OutputTokens = %d, want 63", u.OutputTokens)
	}
}
