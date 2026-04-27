package specgen

import (
	"context"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestRegenerate_HappyPath(t *testing.T) {
	highScore := "SCORE\ncompleteness: 3\ntestability: 3\nscope_coherence: 3\nconstraint_clarity: 2\ntotal: 11\n\nISSUES"

	// Stage 3' feedback draft on codex (cross-model from claude polish),
	// then merge on codex, polish on claude, critique on codex.
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "POLISHED_REGEN"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "FEEDBACK_DRAFT"},
		{Text: "MERGED_REGEN"},
		{Text: highScore},
	}}

	out, err := Regenerate(context.Background(), RegenerateInput{
		OriginalSpec:      "original spec body",
		Feedback:          "task-1 failed; task-2 failed",
		Claude:            claude,
		Codex:             codex,
		PolishEngine:      "claude",
		CritiqueEnabled:   true,
		CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "POLISHED_REGEN" {
		t.Errorf("Final = %q, want POLISHED_REGEN", out.Final)
	}
	if out.Score == nil {
		t.Fatal("Score should be set")
	}
	if out.Score.Total != 11 {
		t.Errorf("Score.Total = %d, want 11", out.Score.Total)
	}
	// Stage 3' feedback draft should go to codex (NOT claude, the polish engine).
	if len(codex.Received) < 1 {
		t.Fatal("codex should have received the feedback draft call")
	}
	if !strings.Contains(codex.Received[0].Prompt, "Failure feedback") {
		t.Error("first codex call should be the feedback draft")
	}
}

func TestRegenerate_CrossModel(t *testing.T) {
	highScore := "SCORE\ncompleteness: 3\ntestability: 3\nscope_coherence: 3\nconstraint_clarity: 3\ntotal: 12\n\nISSUES"

	// When original polish was codex, feedback draft goes to claude.
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "FEEDBACK_DRAFT"},
		{Text: "MERGED_REGEN"},
		{Text: highScore},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "POLISHED_REGEN"},
	}}

	_, err := Regenerate(context.Background(), RegenerateInput{
		OriginalSpec:      "spec",
		Feedback:          "feedback",
		Claude:            claude,
		Codex:             codex,
		PolishEngine:      "codex",
		CritiqueEnabled:   true,
		CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Feedback draft should go to claude (cross-model from codex polish).
	if len(claude.Received) < 1 {
		t.Fatal("claude should have received calls")
	}
	if !strings.Contains(claude.Received[0].Prompt, "Failure feedback") {
		t.Error("first claude call should be the feedback draft")
	}
}

func TestRegenerate_FeedbackDraftError(t *testing.T) {
	// Feedback draft engine has empty script → error.
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{}}

	_, err := Regenerate(context.Background(), RegenerateInput{
		OriginalSpec: "spec",
		Feedback:     "feedback",
		Claude:       claude,
		Codex:        codex,
		PolishEngine: "claude",
	})
	if err == nil {
		t.Fatal("expected error when feedback draft fails")
	}
}
