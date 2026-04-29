package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRoot_GateAIOS_DefaultForUnannotated(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)

	root := NewRootCmd()
	root.SetArgs([]string{"status"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error from `status` outside repo")
	}
	if !strings.Contains(err.Error(), "not a git repo") &&
		!strings.Contains(err.Error(), "not an AIOS repo") {
		t.Fatalf("got error %q; want gate error", err.Error())
	}
}

func TestRoot_GateNone_HelpRunsAnywhere(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	root := NewRootCmd()
	root.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("--help should not error: %v", err)
	}
	if !strings.Contains(buf.String(), "aios") {
		t.Fatalf("help output looks empty: %q", buf.String())
	}
}

func TestRoot_GateGit_DoctorPreRunPasses(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatal(err)
	}
	// Stub out RunE so doctor doesn't actually run engines / call os.Exit.
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	cmd.Run = nil
	root.SetArgs([]string{"doctor"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor gate-rejected in git-only repo: %v", err)
	}
}

func TestRoot_CompletionBackend_RunsAnywhere(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	root := NewRootCmd()
	root.SetArgs([]string{"__complete", "doctor", ""})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("__complete should not be gate-blocked: %v", err)
	}
}

func TestRoot_NoHelpDumpOnError(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)

	root := NewRootCmd()
	root.SetArgs([]string{"status"}) // gate will fail outside repo
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	combined := out.String() + errBuf.String()
	if strings.Contains(combined, "Usage:") {
		t.Fatalf("error output included help dump:\n%s", combined)
	}
	if strings.Contains(combined, "Available Commands:") {
		t.Fatalf("error output included subcommand list:\n%s", combined)
	}
}

func TestRoot_NoHelpDumpOnFlagError(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	root := NewRootCmd()
	root.SetArgs([]string{"run", "--no-such-flag"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	_ = root.Execute()
	combined := out.String() + errBuf.String()
	if strings.Contains(combined, "Usage:") {
		t.Fatalf("flag error triggered help dump:\n%s", combined)
	}
	if strings.Contains(combined, "Available Commands:") {
		t.Fatalf("flag error included subcommand list:\n%s", combined)
	}
}

func TestRoot_NoHelpDumpOnUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	root := NewRootCmd()
	root.SetArgs([]string{"notacommand"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	_ = root.Execute()
	combined := out.String() + errBuf.String()
	if strings.Contains(combined, "Usage:") {
		t.Fatalf("unknown command triggered help dump:\n%s", combined)
	}
}

func TestContinue_HasShortForm(t *testing.T) {
	root := NewRootCmd()
	f := root.Flags().Lookup("continue")
	if f == nil {
		t.Fatal("--continue flag missing on root")
	}
	if f.Shorthand != "c" {
		t.Errorf("--continue shorthand = %q; want %q", f.Shorthand, "c")
	}
}

func TestContinue_NotPersistent(t *testing.T) {
	root := NewRootCmd()
	if root.PersistentFlags().Lookup("continue") != nil {
		t.Error("--continue should not be on PersistentFlags in v0.3")
	}
	statusCmd, _, _ := root.Find([]string{"status"})
	if statusCmd.Flags().Lookup("continue") != nil {
		t.Error("subcommand `status` inherited --continue; should not")
	}
}

func TestContinue_BareDashCBootsLatest(t *testing.T) {
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
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(strings.NewReader("/exit\n"))
	root.SetArgs([]string{"-c"}) // bare -c, no argument

	err := root.Execute()
	// REPL boot will fail (dummy binaries). The key thing: NOT a flag-parse error.
	if err != nil && strings.Contains(err.Error(), "flag needs an argument") {
		t.Fatalf("bare -c errored as flag-needs-argument: %v", err)
	}
}

func TestRoot_ResumeSubcommandOutsideRepo(t *testing.T) {
	// `aios resume task-1` outside a git repo should hit the gate error.
	dir := t.TempDir()
	mustChdir(t, dir)
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"resume", "task-1"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected gate error outside repo")
	}
	if !strings.Contains(err.Error(), "not a git repo") {
		t.Errorf("error %q; want gate error about git repo", err.Error())
	}
}

func TestRoot_ResumeTriesToLoadSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgBody := "schema_version = 1\n[project]\nname = \"x\"\nbase_branch = \"main\"\nstaging_branch = \"aios/staging\"\n[engines]\ncoder_default = \"claude\"\nreviewer_default = \"codex\"\n[engines.claude]\nbinary = \"claude\"\ntimeout_sec = 600\n[engines.codex]\nbinary = \"codex\"\ntimeout_sec = 600\n"
	if err := os.WriteFile(filepath.Join(dir, ".aios", "config.toml"), []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"resume", "task-1"})
	err := root.Execute()
	// Should fail trying to load the session (not a migration hint).
	if err == nil {
		t.Fatal("expected error from `aios resume task-1`")
	}
	if !strings.Contains(err.Error(), "resume") && !strings.Contains(err.Error(), "session") {
		t.Errorf("error %q; want session-related error", err.Error())
	}
}

func TestContinue_DashCWithSpaceSeparatedID(t *testing.T) {
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
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"-c", "session-abc"})
	err := root.Execute()
	// REPL boot will fail because the session file doesn't exist or claude
	// binary missing — what matters is that we did NOT get the
	// "do not combine with a prompt" error and we did NOT treat "session-abc"
	// as a one-shot prompt.
	if err != nil && strings.Contains(err.Error(), "do not combine with a prompt") {
		t.Fatalf("space-separated -c <id> rejected as prompt collision: %v", err)
	}
	// Output should mention session resume attempt OR REPL boot failure,
	// NOT a one-shot prompt or specgen.
	if strings.Contains(buf.String(), "specgen") || strings.Contains(buf.String(), "synthesizing") {
		t.Fatalf("space-separated -c <id> triggered specgen instead of session resume:\n%s", buf.String())
	}
}

func TestPersistent_DryRunYoloRemoved(t *testing.T) {
	root := NewRootCmd()
	for _, name := range []string{"dry-run", "yolo"} {
		if root.PersistentFlags().Lookup(name) != nil {
			t.Errorf("--%s should not be on PersistentFlags in v0.3", name)
		}
	}
	// Subcommands that don't use these flags must NOT see them.
	for _, sub := range []string{"status", "init", "doctor", "cost", "lessons"} {
		c, _, _ := root.Find([]string{sub})
		for _, name := range []string{"dry-run", "yolo"} {
			if c.Flags().Lookup(name) != nil {
				t.Errorf("`%s` should not have --%s in v0.3", sub, name)
			}
		}
	}
	// run and ship MUST have them.
	for _, sub := range []string{"run", "ship"} {
		c, _, _ := root.Find([]string{sub})
		for _, name := range []string{"dry-run", "yolo"} {
			if c.Flags().Lookup(name) == nil {
				t.Errorf("`%s` missing --%s in v0.3", sub, name)
			}
		}
	}
}
