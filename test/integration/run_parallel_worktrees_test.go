package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// TestRunAllParallelRealWorktrees proves that Workers=2+ is safe when each task
// operates in its own `git worktree add` directory. This mirrors the production
// topology used by cli/run.go and confirms there is no .git/index.lock
// contention: worktrees have per-worktree index files, so concurrent git
// add/commit calls don't collide.
func TestRunAllParallelRealWorktrees(t *testing.T) {
	mainDir := initTestRepo(t) // has "main" branch with seed commit

	// Seed shared.txt so the repo has content, then set aios/staging = main.
	seedShared := "A\nB\nC\nD\n"
	if err := os.WriteFile(filepath.Join(mainDir, "shared.txt"), []byte(seedShared), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, mainDir, "git", "add", ".")
	mustRun(t, mainDir, "git", "commit", "-q", "-m", "seed shared")
	mustRun(t, mainDir, "git", "branch", "-f", "aios/staging", "main")

	// Four independent tasks — no DependsOn edges so all four are ready
	// immediately, exercising the Workers=2 pool concurrently.
	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T3", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T4", Status: "pending", Acceptance: []string{"x"}},
	}

	// Worktrees root lives inside the main repo dir (auto-cleaned by t.TempDir).
	wtRoot := filepath.Join(mainDir, ".aios", "worktrees")
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var peak, inflight atomic.Int32

	taskFn := func(ctx context.Context, id orchestrator.TaskID) (orchestrator.TaskResult, *orchestrator.MergeRequest) {
		// Track observed concurrency.
		now := inflight.Add(1)
		for {
			cur := peak.Load()
			if now <= cur || peak.CompareAndSwap(cur, now) {
				break
			}
		}
		defer inflight.Add(-1)

		// Snapshot staging HEAD before we branch — MergeQueue uses this to
		// choose between fast-forward and rebase paths.
		parentSHA := strings.TrimSpace(
			mustRunOut(t, mainDir, "git", "rev-parse", "aios/staging"),
		)

		// Create a real git worktree for this task, branching from parentSHA.
		wtDir := filepath.Join(wtRoot, string(id))
		out, err := exec.Command("git", "-C", mainDir, "worktree", "add",
			"-b", "aios/task/"+string(id), wtDir, parentSHA).CombinedOutput()
		if err != nil {
			t.Errorf("worktree add %s: %v\n%s", id, err, out)
			return orchestrator.TaskResult{ID: id, Status: "blocked"}, nil
		}
		defer func() {
			// Belt-and-suspenders cleanup; t.TempDir removes the whole tree.
			exec.Command("git", "-C", mainDir, "worktree", "remove", "--force", wtDir).Run() //nolint:errcheck
		}()

		// Write a task-unique file — no conflicts possible across tasks.
		path := filepath.Join(wtDir, string(id)+".txt")
		if err := os.WriteFile(path, []byte("hello from "+string(id)+"\n"), 0o644); err != nil {
			t.Errorf("WriteFile %s: %v", id, err)
			return orchestrator.TaskResult{ID: id, Status: "blocked"}, nil
		}

		// Commit inside the worktree (uses its own index; no lock contention).
		mustRun(t, wtDir, "git", "add", ".")
		mustRun(t, wtDir, "git", "commit", "-q", "-m", string(id))

		// Compute diff relative to staging for the MergeQueue ReReview path.
		diff := mustRunOut(t, mainDir, "git", "diff", "aios/staging"+"..aios/task/"+string(id))

		// Hold the slot long enough for a second worker to be observed inflight.
		time.Sleep(50 * time.Millisecond)

		return orchestrator.TaskResult{ID: id, Status: "converged"},
			&orchestrator.MergeRequest{
				TaskID:    id,
				Branch:    "aios/task/" + string(id),
				ParentSHA: parentSHA,
				Diff:      []byte(diff),
			}
	}

	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       mainDir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       2,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	// All four tasks must converge.
	if len(rep.Converged) != 4 {
		t.Errorf("Converged = %d (%v), want 4 (blocked: %v)",
			len(rep.Converged), rep.Converged, rep.Blocked)
	}

	// Confirm we actually observed two workers running simultaneously.
	if peak.Load() < 2 {
		t.Errorf("peak inflight = %d, want >= 2 (parallelism not observed)", peak.Load())
	}

	// Every task's commit message must appear in staging history.
	logOut := mustRunOut(t, mainDir, "git", "log", "--format=%s", "aios/staging")
	for _, tk := range tasks {
		if !strings.Contains(logOut, tk.ID) {
			t.Errorf("staging log missing %s:\n%s", tk.ID, logOut)
		}
	}

	// Report observed peak for debugging convenience.
	t.Logf("peak inflight = %d", peak.Load())
}
