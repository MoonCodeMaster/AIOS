package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	// Doctor would call os.Exit on FAIL — for this test we only verify the
	// gate accepts the command. Invoke PersistentPreRunE directly with a
	// context wired from the parent command.
	cmd.SetContext(root.Context())
	if root.PersistentPreRunE == nil {
		t.Fatal("PersistentPreRunE is nil; expected gate dispatch")
	}
	if err := root.PersistentPreRunE(cmd, []string{}); err != nil {
		t.Fatalf("doctor pre-run rejected in git-only repo: %v", err)
	}
}
