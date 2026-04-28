package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareAIOS_OutsideRepo_PrintsLandingCard(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{}) // bare aios

	err := root.Execute()
	if err != nil {
		t.Fatalf("bare aios outside repo should exit 0; got err: %v", err)
	}
	for _, want := range []string{
		"You're not in an AIOS repo",
		"aios init",
		"aios --help",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("landing card missing %q\n--- output ---\n%s", want, out.String())
		}
	}
}

func TestBareAIOS_InsideAIOSRepo_DoesNotPrintCard(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgBody := "schema_version = 1\n[project]\nname = \"x\"\nbase_branch = \"main\"\nstaging_branch = \"aios/staging\"\n[engines]\ncoder_default = \"claude\"\nreviewer_default = \"codex\"\n[engines.claude]\nbinary = \"claude-not-installed\"\ntimeout_sec = 600\n[engines.codex]\nbinary = \"codex-not-installed\"\ntimeout_sec = 600\n"
	if err := os.WriteFile(filepath.Join(dir, ".aios", "config.toml"), []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{})
	root.SetIn(strings.NewReader("/exit\n"))

	_ = root.Execute()
	// REPL boot will fail because the dummy claude/codex binaries are not on
	// PATH. We only care that the landing card was NOT printed.
	if strings.Contains(out.String(), "You're not in an AIOS repo") {
		t.Fatalf("printed landing card inside an AIOS repo:\n%s", out.String())
	}
}

func TestBareAIOS_WithPromptOutsideRepo_StillErrors(t *testing.T) {
	// `aios "prompt"` outside a repo must error (not landing card).
	// Per spec section 3b: user supplied work, expects work to happen.
	dir := t.TempDir()
	mustChdir(t, dir)

	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"do something"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for `aios \"prompt\"` outside repo")
	}
	if strings.Contains(out.String()+errBuf.String(), "You're not in an AIOS repo") {
		t.Fatal("landing card should not appear when user supplied a prompt")
	}
}
