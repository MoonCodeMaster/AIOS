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
