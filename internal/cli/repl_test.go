package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

// runReplWith runs the REPL with mocked stdin/stdout and the given engines.
func runReplWith(t *testing.T, wd, stdin string, claude, codex engine.Engine) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	in := strings.NewReader(stdin)
	r := &Repl{
		Wd:      wd,
		In:      in,
		Out:     stdout,
		Claude:  claude,
		Codex:   codex,
		NoColor: true,
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return stdout.String()
}

func TestReplOneTurnWritesSpec(t *testing.T) {
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
	// Two lines: the request, then /exit.
	stdin := "build a thing\n\n/exit\n"
	out := runReplWith(t, wd, stdin, claude, codex)

	specBody, err := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if err != nil {
		t.Fatalf("read project.md: %v", err)
	}
	if string(specBody) != "POLISHED" {
		t.Fatalf("project.md = %q, want POLISHED", specBody)
	}
	if !strings.Contains(out, "Spec updated") {
		t.Fatalf("expected 'Spec updated' summary in stdout; got: %s", out)
	}
}
