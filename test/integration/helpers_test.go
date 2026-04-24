package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mustRun runs an arbitrary command in dir and fatals on error.
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// mustRunOut runs an arbitrary command in dir, fatals on error, and returns
// combined stdout+stderr as a string.
func mustRunOut(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// initTestRepo creates a temp dir with a git repo initialised on branch "main"
// with a single seed commit. It does NOT create aios/staging — callers are
// responsible for that so each test can set it up as needed.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-b", "main")
	mustRun(t, dir, "git", "config", "user.email", "t@t")
	mustRun(t, dir, "git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-q", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func seedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-m", "init")
	git(t, dir, "branch", "aios/staging")
	return dir
}

// commitTaskBranch creates branch aios/task/<id> off aios/staging, adds a
// single file commit, and leaves the repo on aios/staging.
// Callers that run this from parallel goroutines must serialize it externally
// (e.g. via a sync.Mutex) because git shares a single working tree.
func commitTaskBranch(t *testing.T, dir, id string) {
	t.Helper()
	git(t, dir, "checkout", "-q", "-b", "aios/task/"+id, "aios/staging")
	if err := os.WriteFile(filepath.Join(dir, id+".txt"), []byte("v\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", id)
	git(t, dir, "checkout", "-q", "aios/staging")
}
