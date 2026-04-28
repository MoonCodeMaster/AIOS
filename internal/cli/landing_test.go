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

func TestBareAIOS_RootReusable_AfterLandingCard(t *testing.T) {
	// Reuse the same root command after the landing-card path fires;
	// a second Execute() in a now-valid AIOS repo must NOT silently no-op.
	dir := t.TempDir()
	mustChdir(t, dir)
	root := NewRootCmd()
	var buf1 bytes.Buffer
	root.SetOut(&buf1)
	root.SetErr(&buf1)
	root.SetArgs([]string{})
	if err := root.Execute(); err != nil {
		t.Fatalf("first Execute (landing card): %v", err)
	}
	if !strings.Contains(buf1.String(), "You're not in an AIOS repo") {
		t.Fatalf("first run did not print landing card:\n%s", buf1.String())
	}

	// Now create a valid AIOS repo and reuse the same root.
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

	var buf2 bytes.Buffer
	root.SetOut(&buf2)
	root.SetErr(&buf2)
	root.SetIn(strings.NewReader("/exit\n"))
	root.SetArgs([]string{})
	err2 := root.Execute()
	// REPL boot will fail because the dummy claude/codex binaries are not on
	// PATH. What we're checking: the second run did NOT silently no-op, which
	// would mean Execute returned nil and the landing card never reprinted.
	if strings.Contains(buf2.String(), "You're not in an AIOS repo") {
		t.Fatalf("second run printed landing card despite valid AIOS repo:\n%s", buf2.String())
	}
	// REPL boot returns an error when the configured CLI binary isn't on PATH.
	// A nil error here indicates the leaked no-op RunE — the bug we're guarding
	// against.
	if err2 == nil {
		t.Fatalf("second Execute returned nil — RunE may be stuck as no-op (buf=%q)", buf2.String())
	}
	if !strings.Contains(err2.Error(), "claude") && !strings.Contains(err2.Error(), "codex") {
		t.Fatalf("second Execute err %q; want REPL-boot error mentioning a CLI binary", err2.Error())
	}
}

func TestBareAIOS_WithExplicitConfig_DoesNotPrintCard(t *testing.T) {
	// `aios --config /path/to/real.toml` should NOT print the landing card,
	// even when cwd has no .aios/. The user explicitly pointed at a config.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "real.toml")
	cfgBody := "schema_version = 1\n[project]\nname = \"x\"\nbase_branch = \"main\"\nstaging_branch = \"aios/staging\"\n[engines]\ncoder_default = \"claude\"\nreviewer_default = \"codex\"\n[engines.claude]\nbinary = \"claude-not-installed\"\ntimeout_sec = 600\n[engines.codex]\nbinary = \"codex-not-installed\"\ntimeout_sec = 600\n"
	if err := os.WriteFile(custom, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", custom})
	root.SetIn(strings.NewReader("/exit\n"))

	_ = root.Execute()
	// REPL boot will fail (dummy binaries). We only assert no landing card.
	if strings.Contains(out.String(), "You're not in an AIOS repo") {
		t.Fatalf("printed landing card despite explicit --config:\n%s", out.String())
	}
}
