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

// TestWorktree_ListAndPruneStale verifies the GC helpers used by `aios run`
// at startup to clean up worktrees left behind by a crashed/killed previous
// run. A non-aios branch worktree in the same repo must not be touched.
func TestWorktree_ListAndPruneStale(t *testing.T) {
	repo := initRepo(t)
	m := &Manager{RepoDir: repo, Root: filepath.Join(repo, ".aios", "worktrees")}

	// Two aios-owned worktrees representing a crashed prior run.
	w1, err := m.Create("task-a", "aios/staging")
	if err != nil {
		t.Fatal(err)
	}
	w2, err := m.Create("task-b", "aios/staging")
	if err != nil {
		t.Fatal(err)
	}

	// A non-aios worktree the user might have created themselves. Must be
	// invisible to List() and therefore survive PruneStale untouched.
	otherDir := filepath.Join(repo, "other-wt")
	if err := exec.Command("git", "-C", repo, "worktree", "add", "-b", "feature/user", otherDir, "aios/staging").Run(); err != nil {
		t.Fatalf("create non-aios worktree: %v", err)
	}

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List returned %d aios worktrees, want 2: %+v", len(list), list)
	}
	byID := map[string]Worktree{}
	for _, w := range list {
		byID[w.TaskID] = w
	}
	if _, ok := byID["task-a"]; !ok {
		t.Errorf("List missing task-a: %+v", list)
	}
	if _, ok := byID["task-b"]; !ok {
		t.Errorf("List missing task-b: %+v", list)
	}

	removed, err := m.PruneStale()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Errorf("PruneStale removed %d, want 2", len(removed))
	}
	if _, err := os.Stat(w1.Path); !os.IsNotExist(err) {
		t.Errorf("task-a worktree still on disk: %v", err)
	}
	if _, err := os.Stat(w2.Path); !os.IsNotExist(err) {
		t.Errorf("task-b worktree still on disk: %v", err)
	}

	// The user's own worktree must be untouched.
	if _, err := os.Stat(otherDir); err != nil {
		t.Errorf("non-aios worktree was pruned (it should not have been): %v", err)
	}

	// Branches must be preserved so history remains inspectable.
	out, err := exec.Command("git", "-C", repo, "branch", "--list", "aios/task/task-a").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "aios/task/task-a") {
		t.Errorf("task-a branch was deleted by PruneStale; want preserved. got: %q", string(out))
	}
}

// seedRepoForTest is a minimal helper for worktree tests; it inits a repo with
// a single seed commit on `main` plus an `aios/staging` branch.
func seedRepoForTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		g := &Git{Dir: dir}
		if _, err := g.Run(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "t@t")
	mustGit("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", ".")
	mustGit("commit", "-m", "init")
	mustGit("branch", "aios/staging")
	return dir
}

// TestDiff_IncludesUncommittedAndUntracked is the regression test for the
// reviewer-diff-empty bug: the coder writes files in the worktree without
// committing, and Diff must surface those changes so the reviewer sees them.
func TestDiff_IncludesUncommittedAndUntracked(t *testing.T) {
	repo := seedRepoForTest(t)
	wm := &Manager{RepoDir: repo, Root: filepath.Join(repo, ".aios", "worktrees")}

	wt, err := wm.Create("001-feat", "aios/staging")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer wm.Remove(wt)

	// Untracked new file (the common coder output).
	if err := os.WriteFile(filepath.Join(wt.Path, "new.go"), []byte("package main\n\nfunc Hello() string { return \"world\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Modify an existing tracked file.
	if err := os.WriteFile(filepath.Join(wt.Path, "README.md"), []byte("hi\n# new heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff, err := wm.Diff(wt, "aios/staging")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, want := range []string{"new.go", "func Hello", "new heading"} {
		if !strings.Contains(diff, want) {
			t.Errorf("Diff is missing %q (the reviewer would not see this work)\n--- diff ---\n%s", want, diff)
		}
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
