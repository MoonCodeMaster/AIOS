package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestOneShotSpecWritesProjectMd(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED_FINAL"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	stdout := &bytes.Buffer{}
	err := runOneShot(context.Background(), OneShotInput{
		Wd: wd, Prompt: "build a thing", Claude: claude, Codex: codex, Out: stdout,
	})
	if err != nil {
		t.Fatalf("runOneShot: %v", err)
	}
	specBody, err := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if err != nil {
		t.Fatalf("read project.md: %v", err)
	}
	if string(specBody) != "POLISHED_FINAL" {
		t.Fatalf("project.md = %q, want POLISHED_FINAL", specBody)
	}
}
