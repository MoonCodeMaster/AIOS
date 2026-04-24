package worktree

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGit_Run_OK(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	g := &Git{Dir: dir}
	out, err := g.Run("rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Errorf("got %q", out)
	}
}

func TestGit_Run_Error(t *testing.T) {
	dir := t.TempDir()
	g := &Git{Dir: dir}
	_, err := g.Run("status")
	if err == nil {
		t.Fatal("expected error outside a git repo")
	}
}
