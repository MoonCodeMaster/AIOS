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

func TestPrintModeWritesToStdoutOnlyNoFile(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED_PRINT"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	stdout := &bytes.Buffer{}
	err := runPrintMode(context.Background(), PrintModeInput{
		Wd: wd, Prompt: "x", Claude: claude, Codex: codex, Out: stdout,
	})
	if err != nil {
		t.Fatalf("runPrintMode: %v", err)
	}
	if got := stdout.String(); got != "POLISHED_PRINT" && got != "POLISHED_PRINT\n" {
		t.Fatalf("stdout = %q, want exactly POLISHED_PRINT (with optional trailing newline)", got)
	}
	// project.md must NOT exist.
	if _, err := os.Stat(filepath.Join(wd, ".aios", "project.md")); !os.IsNotExist(err) {
		t.Fatalf("project.md should not exist in print mode; stat err = %v", err)
	}
}

func TestValidateRootFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		prn  bool
		cont string
		want string // substring of expected error, or "" for nil
	}{
		{"no-args-no-flags-OK", []string{}, false, "", ""},
		{"no-args-with-continue-OK", []string{}, false, "session-x", ""},
		{"no-args-print-rejected", []string{}, true, "", "requires a prompt"},
		{"prompt-no-flags-OK", []string{"build"}, false, "", ""},
		{"prompt-print-OK", []string{"build"}, true, "", ""},
		{"prompt-with-continue-rejected", []string{"build"}, false, "session-x", "REPL-only"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRootFlags(c.args, c.prn, c.cont)
			if c.want == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want err containing %q, got %v", c.want, err)
			}
		})
	}
}
