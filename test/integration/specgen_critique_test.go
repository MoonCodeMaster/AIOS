package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
)

func TestSpecgenCritique_FastPath(t *testing.T) {
	dir := t.TempDir()
	rec, err := run.Open(dir, "test-run")
	if err != nil {
		t.Fatal(err)
	}

	highScore := "SCORE\ncompleteness: 3\ntestability: 3\nscope_coherence: 3\nconstraint_clarity: 2\ntotal: 11\n\nISSUES\n- constraint_clarity: minor gap"

	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"}, {Text: highScore},
	}}

	out, err := specgen.Generate(context.Background(), specgen.Input{
		UserRequest:       "build a widget",
		Claude:            claude,
		Codex:             codex,
		Recorder:          rec,
		CritiqueEnabled:   true,
		CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "POLISHED" {
		t.Errorf("Final = %q, want POLISHED", out.Final)
	}
	if out.Score == nil || out.Score.Total != 11 {
		t.Errorf("Score.Total = %v, want 11", out.Score)
	}
	if out.Refined {
		t.Error("Refined should be false on fast path")
	}

	// Verify audit artifacts.
	critiqueFile := filepath.Join(rec.Root(), "specgen", "5-critique.md")
	if _, err := os.Stat(critiqueFile); err != nil {
		t.Errorf("5-critique.md should exist: %v", err)
	}
	scoreFile := filepath.Join(rec.Root(), "specgen", "5-score.json")
	if _, err := os.Stat(scoreFile); err != nil {
		t.Errorf("5-score.json should exist: %v", err)
	}
	refineFile := filepath.Join(rec.Root(), "specgen", "6-refine.md")
	if _, err := os.Stat(refineFile); !os.IsNotExist(err) {
		t.Error("6-refine.md should NOT exist on fast path")
	}
}

func TestSpecgenCritique_RefinePath(t *testing.T) {
	dir := t.TempDir()
	rec, err := run.Open(dir, "test-run")
	if err != nil {
		t.Fatal(err)
	}

	lowScore := "SCORE\ncompleteness: 2\ntestability: 1\nscope_coherence: 2\nconstraint_clarity: 1\ntotal: 6\n\nISSUES\n- testability: missing assertion\n- constraint_clarity: no error budget"

	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"}, {Text: "REFINED_SPEC"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"}, {Text: lowScore},
	}}

	out, err := specgen.Generate(context.Background(), specgen.Input{
		UserRequest:       "build a widget",
		Claude:            claude,
		Codex:             codex,
		Recorder:          rec,
		CritiqueEnabled:   true,
		CritiqueThreshold: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "REFINED_SPEC" {
		t.Errorf("Final = %q, want REFINED_SPEC", out.Final)
	}
	if !out.Refined {
		t.Error("Refined should be true")
	}

	// Verify audit artifacts.
	refineFile := filepath.Join(rec.Root(), "specgen", "6-refine.md")
	if _, err := os.Stat(refineFile); err != nil {
		t.Errorf("6-refine.md should exist on refine path: %v", err)
	}
	raw, _ := os.ReadFile(refineFile)
	if string(raw) != "REFINED_SPEC" {
		t.Errorf("6-refine.md content = %q, want REFINED_SPEC", string(raw))
	}
}
