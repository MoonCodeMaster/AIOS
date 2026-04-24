package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	seedFile := filepath.Join(dir, "README.md")
	_ = os.WriteFile(seedFile, []byte("hello\n"), 0o644)
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "init"},
		{"branch", "aios/staging"},
	} {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	return dir
}

func TestWorktree_CreateAndRemove(t *testing.T) {
	repo := initRepo(t)
	m := &Manager{RepoDir: repo, Root: filepath.Join(repo, ".aios", "worktrees")}
	wt, err := m.Create("task-001", "aios/staging")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree not on disk: %v", err)
	}
	if err := m.Remove(wt); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}

func TestWorktree_MergeFF(t *testing.T) {
	repo := initRepo(t)
	m := &Manager{RepoDir: repo, Root: filepath.Join(repo, ".aios", "worktrees")}
	wt, err := m.Create("task-001", "aios/staging")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Remove(wt)

	newFile := filepath.Join(wt.Path, "new.txt")
	_ = os.WriteFile(newFile, []byte("content\n"), 0o644)
	wtGit := &Git{Dir: wt.Path}
	if _, err := wtGit.Run("add", "new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wtGit.Run("commit", "-m", "add new"); err != nil {
		t.Fatal(err)
	}

	if err := m.MergeFF(wt, "aios/staging"); err != nil {
		t.Fatal(err)
	}

	// confirm the file landed on staging in the main repo
	repoGit := &Git{Dir: repo}
	out, err := repoGit.Run("show", "aios/staging:new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "content") {
		t.Errorf("merged content missing: %q", out)
	}
}
