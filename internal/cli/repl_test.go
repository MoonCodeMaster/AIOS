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

func TestReplShowPrintsSpec(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, ".aios", "project.md"), []byte("EXISTING_SPEC_BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runReplWith(t, wd, "/show\n\n/exit\n",
		&engine.FakeEngine{Name_: "claude"}, &engine.FakeEngine{Name_: "codex"})
	if !strings.Contains(out, "EXISTING_SPEC_BODY") {
		t.Fatalf("/show did not print spec body; got: %s", out)
	}
}

func TestReplClearDropsTurns(t *testing.T) {
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
	stdin := "first message\n\n/clear\n\n/exit\n"
	out := runReplWith(t, wd, stdin, claude, codex)
	if !strings.Contains(out, "session cleared.") {
		t.Fatalf("/clear did not print confirmation; got: %s", out)
	}
}

func TestReplHelpListsCommands(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := runReplWith(t, wd, "/help\n\n/exit\n",
		&engine.FakeEngine{Name_: "claude"}, &engine.FakeEngine{Name_: "codex"})
	for _, expected := range []string{"/show", "/clear", "/ship", "/exit"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("/help missing %s; got: %s", expected, out)
		}
	}
}
