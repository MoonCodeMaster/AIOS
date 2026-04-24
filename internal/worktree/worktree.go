package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// taskBranchPrefix is the namespace this package owns inside the repo's refs.
// Every aios-created worktree lives on a branch under this prefix; List and
// PruneStale both use it as the filter that separates "ours" from "someone
// else's worktrees" so GC never touches a user's own branches.
const taskBranchPrefix = "aios/task/"

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

// List returns every worktree in the repo that belongs to AIOS — identified
// by a branch with the "aios/task/" prefix. Used by PruneStale at startup to
// GC orphans left behind by a previous run that crashed or was SIGKILLed
// before `defer Remove` could fire.
//
// The primary repo checkout (without a task branch) is excluded even if git
// reports it, because removing it would wipe the user's working tree.
func (m *Manager) List() ([]Worktree, error) {
	g := &Git{Dir: m.RepoDir}
	out, err := g.Run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []Worktree
	var cur Worktree
	flush := func() {
		if cur.Path != "" && strings.HasPrefix(cur.Branch, taskBranchPrefix) {
			cur.TaskID = strings.TrimPrefix(cur.Branch, taskBranchPrefix)
			wts = append(wts, cur)
		}
		cur = Worktree{}
	}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(ln, "worktree "):
			cur.Path = strings.TrimPrefix(ln, "worktree ")
		case strings.HasPrefix(ln, "branch "):
			// git emits refs/heads/aios/task/<id>; normalize to a short branch
			ref := strings.TrimPrefix(ln, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
	}
	flush()
	return wts, nil
}

// PruneStale removes every aios-owned worktree in this repo. Safe to call at
// startup of `aios run` — any aios/task/* worktree present at that moment is
// by definition an orphan from a crashed/killed previous run, because AIOS
// itself is not yet working on anything. Branches are preserved (never
// deleted) so historical per-task work remains inspectable via
// `git log aios/task/<id>`. Best-effort on per-worktree errors: continues
// on failure and returns an aggregated error at the end.
func (m *Manager) PruneStale() ([]Worktree, error) {
	wts, err := m.List()
	if err != nil {
		return nil, err
	}
	var removed []Worktree
	var errs []string
	for _, w := range wts {
		wt := w
		if err := m.Remove(&wt); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", w.TaskID, err))
			continue
		}
		removed = append(removed, wt)
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("prune stale: %s", strings.Join(errs, "; "))
	}
	return removed, nil
}
