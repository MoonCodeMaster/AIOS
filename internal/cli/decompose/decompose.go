// Package decompose implements the M2 auto-decompose handler: parallel
// Claude+Codex proposals + synthesizer (= reviewer of stuck task) merging.
//
// Failure modes (all return ErrAbandon):
//   - Both proposals errored.
//   - Synthesis returned ≤ 1 sub-task.
//   - Any sub-task ID ends in ".giveup".
//   - Synthesis output had no parseable sub-task blocks.
//
// Single-source fallback (one proposal errored, the other succeeded): skip
// synthesis, use the surviving proposal directly. Out.UsedSynthesizer = false.
//
// Synthesizer fallback (both proposals OK, synthesizer errored): deterministic
// union with id dedupe. On collision, prefer the proposal whose author was
// the synthesizer (= reviewer of the stuck task).
package decompose

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// ErrAbandon signals the caller that the decompose handler gave up — no
// children were produced. Caller should fall through to the M1 abandon path.
var ErrAbandon = errors.New("decompose: abandoned")

// Input carries everything Run needs.
type Input struct {
	Parent          *spec.Task
	Claude          engine.Engine
	Codex           engine.Engine
	Synthesizer     engine.Engine
	SynthesizerName string // "claude" | "codex" — used for the fallback-merge tiebreaker
	IssuesByRound   [][]string
	LastDiff        string
}

// Output is the result of a successful decompose.
type Output struct {
	Children        []*spec.Task
	UsedSynthesizer bool
}

// Run dispatches the dual-engine decompose. Returns ErrAbandon (wrapped) on
// any failure path that the caller should treat as "decompose declined".
func Run(ctx context.Context, in Input) (Output, error) {
	if in.Parent == nil || in.Claude == nil || in.Codex == nil || in.Synthesizer == nil {
		return Output{}, fmt.Errorf("decompose: nil parent or engine in Input")
	}
	stuckPrompt, err := renderStuckPrompt(in)
	if err != nil {
		return Output{}, fmt.Errorf("render decompose-stuck: %w", err)
	}
	req := engine.InvokeRequest{Role: engine.RoleCoder, Prompt: stuckPrompt}
	ra, rb := engine.InvokeParallel(ctx, in.Claude, in.Codex, req, req)

	switch {
	case ra.Err != nil && rb.Err != nil:
		return Output{}, fmt.Errorf("%w: both proposals errored: claude=%v, codex=%v", ErrAbandon, ra.Err, rb.Err)
	case ra.Err != nil:
		return parseAndStamp(in.Parent, rb.Response.Text, false)
	case rb.Err != nil:
		return parseAndStamp(in.Parent, ra.Response.Text, false)
	}

	mergePrompt, err := renderMergePrompt(in, ra.Response.Text, rb.Response.Text)
	if err != nil {
		return Output{}, fmt.Errorf("render decompose-merge: %w", err)
	}
	mr, mergeErr := in.Synthesizer.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: mergePrompt})
	if mergeErr != nil {
		merged := unionDedupe(ra.Response.Text, rb.Response.Text, in.SynthesizerName)
		return parseAndStamp(in.Parent, merged, false)
	}
	return parseAndStamp(in.Parent, mr.Text, true)
}

func renderStuckPrompt(in Input) (string, error) {
	dedup := dedupeIssues(in.IssuesByRound)
	return prompts.Render("decompose-stuck.tmpl", map[string]any{
		"ParentID":   in.Parent.ID,
		"ParentBody": in.Parent.Body,
		"Issues":     dedup,
		"LastDiff":   truncateLines(in.LastDiff, 200),
		"Acceptance": in.Parent.Acceptance,
	})
}

func renderMergePrompt(in Input, propA, propB string) (string, error) {
	return prompts.Render("decompose-merge.tmpl", map[string]any{
		"ParentID":   in.Parent.ID,
		"ParentBody": in.Parent.Body,
		"ProposalA":  propA,
		"ProposalB":  propB,
	})
}

func dedupeIssues(byRound [][]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, round := range byRound {
		for _, issue := range round {
			if _, ok := seen[issue]; ok {
				continue
			}
			seen[issue] = struct{}{}
			out = append(out, issue)
		}
	}
	return out
}

func truncateLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// parseAndStamp parses ===TASK===-separated frontmatter blocks, re-stamps the
// ID as <parent>.<n>, sets Depth=parent.Depth+1, and returns the children.
// Returns ErrAbandon if fewer than 2 valid sub-tasks parse, or if any block's
// ID ends in ".giveup".
func parseAndStamp(parent *spec.Task, raw string, usedSynthesizer bool) (Output, error) {
	parts := strings.Split(raw, "\n===TASK===\n")
	var children []*spec.Task
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		t, err := spec.ParseTask(p)
		if err != nil {
			continue
		}
		if strings.HasSuffix(t.ID, ".giveup") {
			return Output{}, fmt.Errorf("%w: model emitted .giveup marker", ErrAbandon)
		}
		t.ID = fmt.Sprintf("%s.%d", parent.ID, i+1)
		t.ParentID = parent.ID
		t.Depth = parent.Depth + 1
		if t.Status == "" {
			t.Status = "pending"
		}
		children = append(children, t)
	}
	if len(children) < 2 {
		return Output{}, fmt.Errorf("%w: only %d sub-task(s) parsed; minimum is 2", ErrAbandon, len(children))
	}
	return Output{Children: children, UsedSynthesizer: usedSynthesizer}, nil
}

// unionDedupe is the synthesizer-fallback merge: concatenate both proposals,
// drop duplicate ===TASK=== blocks by id. On id collision, prefer the block
// from the proposal whose author is `preferAuthor` ("claude" or "codex").
func unionDedupe(propA, propB, preferAuthor string) string {
	first, second := propA, propB
	if preferAuthor == "codex" {
		first, second = propB, propA
	}
	seenIDs := map[string]struct{}{}
	var out []string
	for _, p := range strings.Split(first, "\n===TASK===\n") {
		id := extractID(p)
		if id == "" {
			continue
		}
		seenIDs[id] = struct{}{}
		out = append(out, strings.TrimSpace(p))
	}
	for _, p := range strings.Split(second, "\n===TASK===\n") {
		id := extractID(p)
		if id == "" {
			continue
		}
		if _, dup := seenIDs[id]; dup {
			continue
		}
		seenIDs[id] = struct{}{}
		out = append(out, strings.TrimSpace(p))
	}
	return strings.Join(out, "\n===TASK===\n")
}

func extractID(block string) string {
	for _, ln := range strings.Split(block, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "id:") {
			id := strings.TrimSpace(strings.TrimPrefix(ln, "id:"))
			id = strings.Trim(id, `"'`)
			return id
		}
	}
	return ""
}
