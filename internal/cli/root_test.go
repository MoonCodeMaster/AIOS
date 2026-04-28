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
