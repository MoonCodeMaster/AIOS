package specgen

import (
	"context"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func highScoreCritique() string {
	return `SCORE
completeness: 3
testability: 3
scope_coherence: 3
constraint_clarity: 2
total: 11

ISSUES
- constraint_clarity: no error budget specified`
}

func lowScoreCritique() string {
	return `SCORE
completeness: 2
testability: 1
scope_coherence: 2
constraint_clarity: 1
total: 6

ISSUES
- testability: acceptance criterion 3 lacks a measurable assertion
- constraint_clarity: no error budget specified`
}

func TestCritique_HighScore_NoRefine(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"}, {Text: highScoreCritique()},
	}}
	out, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: true, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Score == nil {
		t.Fatal("Score should be set")
	}
	if out.Score.Total != 11 {
		t.Errorf("Total = %d, want 11", out.Score.Total)
	}
	if !out.Score.Pass {
		t.Error("Pass should be true for score 11 >= threshold 9")
	}
	if out.Refined {
		t.Error("Refined should be false when score passes")
	}
	if out.Final != "POLISHED" {
		t.Errorf("Final = %q, want POLISHED", out.Final)
	}
	// Critique should have run on codex (cross-model from claude polish).
	if len(codex.Received) != 3 {
		t.Errorf("codex calls = %d, want 3 (draft + merge + critique)", len(codex.Received))
	}
	// Total engine calls: 4 original + 1 critique = 5.
	total := len(claude.Received) + len(codex.Received)
	if total != 5 {
		t.Errorf("total engine calls = %d, want 5", total)
	}
}

func TestCritique_LowScore_RefinesFires(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"}, {Text: "REFINED_SPEC"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"}, {Text: lowScoreCritique()},
	}}
	out, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: true, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Score == nil {
		t.Fatal("Score should be set")
	}
	if out.Score.Total != 6 {
		t.Errorf("Total = %d, want 6", out.Score.Total)
	}
	if out.Score.Pass {
		t.Error("Pass should be false for score 6 < threshold 9")
	}
	if !out.Refined {
		t.Error("Refined should be true")
	}
	if out.Final != "REFINED_SPEC" {
		t.Errorf("Final = %q, want REFINED_SPEC", out.Final)
	}
	if len(out.CritiqueIssues) != 2 {
		t.Errorf("CritiqueIssues = %d, want 2", len(out.CritiqueIssues))
	}
	// Refine runs on claude (polish engine). Total: 4 + 1 critique + 1 refine = 6.
	total := len(claude.Received) + len(codex.Received)
	if total != 6 {
		t.Errorf("total engine calls = %d, want 6", total)
	}
}

func TestCritique_EngineError_ReturnsPolished(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	// Codex: draft + merge succeed, critique call exhausts script → error.
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	out, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: true, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "POLISHED" {
		t.Errorf("Final = %q, want POLISHED (fallback on critique error)", out.Final)
	}
	if out.Score != nil {
		t.Error("Score should be nil on critique error")
	}
	hasWarning := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "critique engine failed") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("should have warning about critique failure")
	}
}

func TestCritique_RefineError_ReturnsPolished(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
		// Refine call exhausts script → error.
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"}, {Text: lowScoreCritique()},
	}}
	out, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: true, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "POLISHED" {
		t.Errorf("Final = %q, want POLISHED (fallback on refine error)", out.Final)
	}
	if out.Refined {
		t.Error("Refined should be false when refine fails")
	}
}

func TestCritique_Disabled_SkipsStages(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	out, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: false, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "POLISHED" {
		t.Errorf("Final = %q, want POLISHED", out.Final)
	}
	if out.Score != nil {
		t.Error("Score should be nil when critique disabled")
	}
	// Only 4 engine calls (no critique, no refine).
	total := len(claude.Received) + len(codex.Received)
	if total != 4 {
		t.Errorf("total engine calls = %d, want 4", total)
	}
}

func TestCritique_CrossModel_ClaudePolishCodexCritique(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"}, {Text: highScoreCritique()},
	}}
	_, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: true, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The 3rd codex call should be the critique (contains "Score the following spec").
	if len(codex.Received) < 3 {
		t.Fatalf("codex calls = %d, want >= 3", len(codex.Received))
	}
	critiqueReq := codex.Received[2]
	if !strings.Contains(critiqueReq.Prompt, "Score the following spec") {
		t.Error("critique prompt should have been sent to codex (cross-model from claude polish)")
	}
}

func TestCritique_CrossModel_CodexPolishClaudeCritique(t *testing.T) {
	// Claude draft fails (FailOnCall=1), so Codex becomes the surviving
	// engine and runs polish. Critique must then route to Claude.
	claude := &engine.FailOnCallEngine{
		Name_:      "claude",
		FailOnCall: 1,
		Script: []engine.InvokeResponse{
			{},                          // placeholder for the failed call slot
			{Text: highScoreCritique()}, // critique response
		},
	}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"},       // stage 2: codex draft
		{Text: "POLISHED_BY_X"}, // stage 4: codex polish (single-draft fallback)
	}}
	out, err := Generate(context.Background(), Input{
		Claude: claude, Codex: codex,
		CritiqueEnabled: true, CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Score == nil || out.Score.Total != 11 {
		t.Errorf("Score.Total = %v, want 11", out.Score)
	}
	if out.Refined {
		t.Error("Refined should be false on fast path")
	}
	// Claude should have received the critique call (call 2, after draft failed on call 1).
	if len(claude.Received) < 2 {
		t.Fatalf("claude calls = %d, want >= 2", len(claude.Received))
	}
	last := claude.Received[len(claude.Received)-1]
	if !strings.Contains(last.Prompt, "Score the following spec") {
		t.Error("critique prompt should have been sent to claude (cross-model from codex polish)")
	}
}
