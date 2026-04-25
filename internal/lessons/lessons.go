// Package lessons mines .aios/runs/* for recurring reviewer-issue
// fingerprints. The output is a small report — top categories, top notes,
// top file hot-spots — that the user can read in 30 seconds and decide
// what to feed back into the spec, the coder prompt, or the codebase
// itself.
//
// This is the opposite operation to the cost package: cost asks "what did
// we spend?", lessons asks "what did the spend buy us in terms of
// recurring problems?". Both read the same on-disk audit; neither
// requires a model call.
package lessons

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Issue is the on-disk reviewer-issue shape, duplicated here so this
// package does not import the orchestrator (which would create a cycle —
// the orchestrator pulls in everything else).
type Issue struct {
	Severity string `json:"severity"`
	Category string `json:"category,omitempty"`
	Note     string `json:"note"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// reviewerResponse is the on-disk shape AIOS persists per round.
type reviewerResponse struct {
	Approved bool    `json:"approved"`
	Issues   []Issue `json:"issues"`
}

// Report is the consolidated mining output.
type Report struct {
	TotalRuns      int
	TotalIssues    int
	TotalBlocking  int
	ByCategory     []Bucket  // sorted desc by count
	ByNoteShape    []Bucket  // sorted desc by count
	HotSpotFiles   []Bucket  // sorted desc by count
	BiggestRunDirs []Bucket  // run-dir → issue count, sorted desc
}

type Bucket struct {
	Key   string
	Count int
}

// Mine walks runsDir (typically .aios/runs/) and aggregates every
// reviewer-response.json it finds. Files that are not parseable JSON or
// that are missing the "issues" field are silently skipped — the report
// is supposed to be useful even on a half-corrupt archive.
func Mine(runsDir string) (Report, error) {
	rep := Report{}
	categoryCount := map[string]int{}
	noteCount := map[string]int{}
	fileCount := map[string]int{}
	runIssueCount := map[string]int{}
	runs := map[string]bool{}

	// "no runs dir" is a config mistake, not "no issues found" — surface it.
	if _, err := os.Stat(runsDir); err != nil {
		return rep, err
	}
	err := filepath.WalkDir(runsDir, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if filepath.Base(path) != "reviewer-response.json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var resp reviewerResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil
		}
		runID := runIDFromPath(runsDir, path)
		if runID != "" {
			runs[runID] = true
		}
		for _, iss := range resp.Issues {
			rep.TotalIssues++
			if iss.Severity == "blocking" {
				rep.TotalBlocking++
			}
			cat := iss.Category
			if cat == "" {
				cat = "(unspecified)"
			}
			categoryCount[cat]++
			if iss.Note != "" {
				noteCount[noteShape(iss.Note)]++
			}
			if iss.File != "" {
				fileCount[iss.File]++
			}
			if runID != "" {
				runIssueCount[runID]++
			}
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	rep.TotalRuns = len(runs)
	rep.ByCategory = topN(categoryCount, 10)
	rep.ByNoteShape = topN(noteCount, 15)
	rep.HotSpotFiles = topN(fileCount, 10)
	rep.BiggestRunDirs = topN(runIssueCount, 5)
	return rep, nil
}

// noteShape collapses minor text variations so two notes that differ only
// in a token count, file path, or line number cluster together. The
// algorithm is deliberately blunt: lowercase, replace digits with #, trim
// to 80 chars, strip trailing punctuation. A real NLP-ish dedupe would be
// nicer; this is enough to surface the obvious recurring patterns.
func noteShape(note string) string {
	s := strings.ToLower(note)
	var b strings.Builder
	prevDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !prevDigit {
				b.WriteByte('#')
			}
			prevDigit = true
			continue
		}
		prevDigit = false
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimRight(out, ".,;:!?\"'`")
	if len(out) > 80 {
		out = out[:80] + "…"
	}
	return out
}

// runIDFromPath extracts the timestamp segment after the runsDir prefix.
// Given runsDir=".aios/runs" and path=".aios/runs/2026-04-26T01-02-03/task/round-1/reviewer-response.json"
// returns "2026-04-26T01-02-03". Returns empty string when path does not
// fall under runsDir.
func runIDFromPath(runsDir, path string) string {
	rel, err := filepath.Rel(runsDir, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 || parts[0] == "." || parts[0] == ".." {
		return ""
	}
	return parts[0]
}

// topN sorts m by count desc and returns up to n entries. Stable on key
// for ties so identical inputs produce identical reports.
func topN(m map[string]int, n int) []Bucket {
	out := make([]Bucket, 0, len(m))
	for k, v := range m {
		out = append(out, Bucket{Key: k, Count: v})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Render prints the report as a terminal-friendly summary.
func (r Report) Render(w io.Writer) {
	bar := strings.Repeat("─", 78)
	fmt.Fprintln(w, "lessons learned")
	fmt.Fprintln(w, bar)
	fmt.Fprintf(w, "%d run(s) scanned · %d total reviewer issues · %d blocking\n",
		r.TotalRuns, r.TotalIssues, r.TotalBlocking)
	if r.TotalIssues == 0 {
		fmt.Fprintln(w, bar)
		fmt.Fprintln(w, "no issues found — either the runs were perfect or the audit is empty.")
		return
	}
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "top categories:")
	renderBuckets(w, r.ByCategory)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "top recurring notes:")
	renderBuckets(w, r.ByNoteShape)
	if len(r.HotSpotFiles) > 0 {
		fmt.Fprintln(w, bar)
		fmt.Fprintln(w, "hot-spot files:")
		renderBuckets(w, r.HotSpotFiles)
	}
	if len(r.BiggestRunDirs) > 0 {
		fmt.Fprintln(w, bar)
		fmt.Fprintln(w, "noisiest runs:")
		renderBuckets(w, r.BiggestRunDirs)
	}
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "interpretation:")
	fmt.Fprintln(w, "  - Categories that dominate suggest a missing constraint in coder.tmpl.")
	fmt.Fprintln(w, "  - Notes that recur across runs suggest a class of bug your spec is silent on.")
	fmt.Fprintln(w, "  - Hot-spot files suggest a refactor would pay back review-loop time.")
}

func renderBuckets(w io.Writer, b []Bucket) {
	if len(b) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	maxKey := 0
	for _, e := range b {
		if len(e.Key) > maxKey {
			maxKey = len(e.Key)
		}
	}
	for _, e := range b {
		pad := strings.Repeat(" ", maxKey-len(e.Key))
		fmt.Fprintf(w, "  %s%s  %4d\n", e.Key, pad, e.Count)
	}
}
