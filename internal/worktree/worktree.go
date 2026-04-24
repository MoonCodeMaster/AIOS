package worktree

import (
	"fmt"
	"os"
	"path/filepath"
)

type Worktree struct {
	TaskID string
	Branch string // e.g. aios/task/001-foo
	Path   string
}

type Manager struct {
	RepoDir string // the primary checkout
	Root    string // where worktrees live (default .aios/worktrees)
}

func (m *Manager) Create(taskID, fromBranch string) (*Worktree, error) {
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir worktrees root: %w", err)
	}
	branch := "aios/task/" + taskID
	path := filepath.Join(m.Root, taskID)
	g := &Git{Dir: m.RepoDir}
	if _, err := g.Run("worktree", "add", "-b", branch, path, fromBranch); err != nil {
		return nil, err
	}
	return &Worktree{TaskID: taskID, Branch: branch, Path: path}, nil
}

func (m *Manager) Remove(w *Worktree) error {
	g := &Git{Dir: m.RepoDir}
	if _, err := g.Run("worktree", "remove", "--force", w.Path); err != nil {
		return err
	}
	return nil
}

// MergeFF fast-forwards target onto the worktree's branch. If ff is not possible,
// returns an error (do NOT rebase or re-try in v0).
func (m *Manager) MergeFF(w *Worktree, target string) error {
	g := &Git{Dir: m.RepoDir}
	// switch primary checkout to target temporarily via `git update-ref` to avoid
	// perturbing user's current branch: easier to use `git merge` from a detached state.
	// Simpler: use `git fetch . <branch>:<target>` which only succeeds when fast-forward.
	if _, err := g.Run("fetch", ".", fmt.Sprintf("%s:%s", w.Branch, target)); err != nil {
		return fmt.Errorf("non-fast-forward merge %s → %s: %w", w.Branch, target, err)
	}
	return nil
}

// Diff returns the patch introduced on the worktree's branch since fromBranch.
func (m *Manager) Diff(w *Worktree, fromBranch string) (string, error) {
	g := &Git{Dir: m.RepoDir}
	return g.Run("diff", fromBranch+".."+w.Branch)
}
