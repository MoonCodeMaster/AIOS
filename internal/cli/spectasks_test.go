package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestShipPromptWritesSpecThenShips(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	called := false
	res, err := ShipPrompt(context.Background(), ShipPromptInput{
		Wd: wd, Prompt: "build a thing", Claude: claude, Codex: codex,
		ShipSpecFn: func(_ context.Context, w string) (ShipResult, error) {
			data, err := os.ReadFile(filepath.Join(w, ".aios", "project.md"))
			if err != nil {
				return ShipResult{}, err
			}
			if string(data) != "POLISHED" {
				t.Fatalf("ShipSpec saw spec %q, want POLISHED", data)
			}
			called = true
			return ShipResult{Status: ShipMerged, PRURL: "https://example/pr/1", PRNumber: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("ShipPrompt: %v", err)
	}
	if !called {
		t.Fatalf("ShipSpecFn was not called")
	}
	if res.Status != ShipMerged || res.PRNumber != 1 {
		t.Fatalf("Result = %+v", res)
	}
}

func TestShipPromptSpecgenError(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &errEngineCli{err: errors.New("claude offline")}
	codex := &errEngineCli{err: errors.New("codex offline")}
	_, err := ShipPrompt(context.Background(), ShipPromptInput{
		Wd: wd, Prompt: "x", Claude: claude, Codex: codex,
	})
	if err == nil {
		t.Fatalf("expected error when both drafters fail")
	}
	// Spec must NOT have been written when specgen fails.
	if _, statErr := os.Stat(filepath.Join(wd, ".aios", "project.md")); !os.IsNotExist(statErr) {
		t.Fatalf("project.md should not exist after specgen failure; stat err = %v", statErr)
	}
}

// errEngineCli is a local error-only Engine for cli-package tests.
type errEngineCli struct{ err error }

func (e *errEngineCli) Name() string { return "errEngineCli" }
func (e *errEngineCli) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	return nil, e.err
}
