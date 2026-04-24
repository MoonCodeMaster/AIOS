package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/orchestrator"
	"github.com/Solaxis/aios/internal/spec"
)

// TestRunAllRebaseConflictBlocks: two tasks both edit the same line starting
// from the same staging HEAD. T1 lands via FF; T2 fails with rebase-conflict.
// All inline FF/rebase logic is removed — the Task callback returns a
// MergeRequest and RunAll routes it through the MergeQueue.
func TestRunAllRebaseConflictBlocks(t *testing.T) {
	dir := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "seed")
	mustRun(t, dir, "git", "branch", "-f", "aios/staging", "main")

	// Capture the original staging HEAD once, before any task runs.
	// Both task branches are rooted here so that when the second task tries to
	// rebase onto staging (which has T1's commit), it conflicts.
	origHead := strings.TrimSpace(mustRunOut(t, dir, "git", "rev-parse", "aios/staging"))

	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
	}
	taskFn := func(ctx context.Context, id orchestrator.TaskID) (orchestrator.TaskResult, *orchestrator.MergeRequest) {
		// Branch from the original staging HEAD (not the current one).
		// This ensures T2 conflicts with T1 even though tasks are serialized.
		mustRun(t, dir, "git", "checkout", "-q", "-b", "aios/task/"+string(id), origHead)
		// Both tasks change the same line — guaranteed rebase conflict when
		// T2 tries to rebase onto staging after T1 has been merged.
		_ = os.WriteFile(filepath.Join(dir, "shared.txt"), []byte(string(id)+"\n"), 0o644)
		mustRun(t, dir, "git", "add", ".")
		mustRun(t, dir, "git", "commit", "-q", "-m", string(id))

		// Return to staging so MergeQueue can operate on the working tree.
		mustRun(t, dir, "git", "checkout", "-q", "aios/staging")

		// Delegate merge to RunAll's MergeQueue via MergeRequest.
		// The "blocked" reason ("rebase-conflict") will come from MergeQueue.
		return orchestrator.TaskResult{ID: id, Status: "converged"}, &orchestrator.MergeRequest{
			TaskID:    id,
			Branch:    "aios/task/" + string(id),
			ParentSHA: origHead,
			Diff:      nil,
		}
	}
	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1, // Serialized: T1 merges first, then T2 conflicts.
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	// Exactly one converged, one blocked.
	if len(rep.Converged)+len(rep.Blocked) != 2 {
		t.Fatalf("totals = %d converged + %d blocked", len(rep.Converged), len(rep.Blocked))
	}
	if len(rep.Converged) != 1 || len(rep.Blocked) != 1 {
		t.Errorf("expected 1 converged + 1 blocked, got %v / %v", rep.Converged, rep.Blocked)
	}
}
