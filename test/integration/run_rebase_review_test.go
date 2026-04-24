package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// setupRebaseReviewRepo creates a repo with shared.txt that has 5 lines.
// T1 inserts a new line after line1 (shifts later lines down).
// T2 changes line5 to "T2-line5".
// Because both tasks branch from origHead, T2 must rebase onto T1's staging.
// After rebase, T2's hunk header changes (line5 is now line6), so the diff
// bytes differ from T2's original diff — triggering ReReview.
//
// Returns: dir, origHead, and two "apply" functions (one per task).
func setupRebaseReviewRepo(t *testing.T) (dir, origHead string, applyT1, applyT2 func()) {
	t.Helper()
	dir = initTestRepo(t)

	// shared.txt: 5 lines so T1 and T2 can edit non-overlapping regions.
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "seed")
	mustRun(t, dir, "git", "branch", "-f", "aios/staging", "main")

	origHead = strings.TrimSpace(mustRunOut(t, dir, "git", "rev-parse", "aios/staging"))

	// T1: insert a new line after line1 (shifts line5 from position 5 to 6).
	applyT1 = func() {
		if err := os.WriteFile(filepath.Join(dir, "shared.txt"),
			[]byte("line1\nT1-insert\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// T2: change line5 to "T2-line5" (from original parent — no T1-insert).
	applyT2 = func() {
		if err := os.WriteFile(filepath.Join(dir, "shared.txt"),
			[]byte("line1\nline2\nline3\nline4\nT2-line5\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir, origHead, applyT1, applyT2
}

// setupNoChangeRepo creates a repo where T1 edits f1.txt and T2 edits f2.txt.
// T2's rebased diff is byte-for-byte identical to its original diff because
// T1's changes are in a completely different file (no hunk-header shift).
//
// Returns: dir, origHead, and two "apply" functions.
func setupNoChangeRepo(t *testing.T) (dir, origHead string, applyT1, applyT2 func()) {
	t.Helper()
	dir = initTestRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("f1-content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("f2-content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "seed")
	mustRun(t, dir, "git", "branch", "-f", "aios/staging", "main")

	origHead = strings.TrimSpace(mustRunOut(t, dir, "git", "rev-parse", "aios/staging"))

	applyT1 = func() {
		if err := os.WriteFile(filepath.Join(dir, "f1.txt"), []byte("f1-T1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	applyT2 = func() {
		if err := os.WriteFile(filepath.Join(dir, "f2.txt"), []byte("f2-T2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir, origHead, applyT1, applyT2
}

// buildTaskFn constructs a serialized TaskFn for two tasks. Each task:
//   - Branches from origHead (so T2 always needs to rebase when T1 lands first).
//   - Applies its edit via the corresponding applyFn.
//   - Commits and returns to aios/staging.
//   - Returns a MergeRequest with the captured diff and the provided reReview callback.
//
// A mutex is held for the duration of each task's git operations because all
// tests use Workers:1 (single shared working tree), but the mutex is here for
// clarity and safety.
func buildTaskFn(
	t *testing.T,
	dir, origHead string,
	applyFns map[string]func(),
	reReview func([]byte) (bool, error),
) orchestrator.TaskFn {
	var mu sync.Mutex
	return func(ctx context.Context, id orchestrator.TaskID) (orchestrator.TaskResult, *orchestrator.MergeRequest) {
		mu.Lock()
		defer mu.Unlock()

		apply, ok := applyFns[string(id)]
		if !ok {
			t.Errorf("no apply function for task %s", id)
			return orchestrator.TaskResult{ID: id, Status: "blocked"}, nil
		}

		// Create a branch rooted at origHead (not the current staging tip).
		// This guarantees T2 always requires a rebase when T1 has already landed.
		mustRun(t, dir, "git", "checkout", "-q", "-b", "aios/task/"+string(id), origHead)
		apply()
		mustRun(t, dir, "git", "add", ".")
		mustRun(t, dir, "git", "commit", "-q", "-m", string(id))

		// Capture T2's diff against its parent (origHead) BEFORE rebase.
		// MergeQueue compares this against the post-rebase diff to decide
		// whether to invoke ReReview.
		rawDiff := mustRunOut(t, dir, "git", "diff", origHead+"..HEAD")

		// Return to staging so MergeQueue can operate on the shared working tree.
		mustRun(t, dir, "git", "checkout", "-q", "aios/staging")

		return orchestrator.TaskResult{ID: id, Status: "converged"},
			&orchestrator.MergeRequest{
				TaskID:    id,
				Branch:    "aios/task/" + string(id),
				ParentSHA: origHead,
				Diff:      []byte(rawDiff),
				ReReview:  reReview,
			}
	}
}

// TestRebaseReviewCalledOnDiffChange: after T1 shifts T2's hunk headers,
// the post-rebase diff bytes differ from T2's original diff, so ReReview
// must be called exactly once.
func TestRebaseReviewCalledOnDiffChange(t *testing.T) {
	dir, origHead, applyT1, applyT2 := setupRebaseReviewRepo(t)

	var reReviewCalls atomic.Int32
	reReview := func(newDiff []byte) (bool, error) {
		reReviewCalls.Add(1)
		return true, nil // approve so the test can converge cleanly
	}

	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
	}
	applyFns := map[string]func(){"T1": applyT1, "T2": applyT2}
	taskFn := buildTaskFn(t, dir, origHead, applyFns, reReview)

	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(rep.Blocked) != 0 {
		t.Errorf("expected no blocked tasks, got %v", rep.Blocked)
	}
	if got := reReviewCalls.Load(); got != 1 {
		t.Errorf("ReReview call count = %d, want 1", got)
	}
}

// TestRebaseReviewApproves: when ReReview returns (true, nil), the rebased
// task should converge and its commit should appear in staging.
func TestRebaseReviewApproves(t *testing.T) {
	dir, origHead, applyT1, applyT2 := setupRebaseReviewRepo(t)

	reReview := func(newDiff []byte) (bool, error) {
		return true, nil
	}

	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
	}
	applyFns := map[string]func(){"T1": applyT1, "T2": applyT2}
	taskFn := buildTaskFn(t, dir, origHead, applyFns, reReview)

	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(rep.Blocked) != 0 {
		t.Errorf("expected no blocked tasks, got %v", rep.Blocked)
	}
	if len(rep.Converged) != 2 {
		t.Errorf("expected 2 converged, got %v", rep.Converged)
	}

	// Both commits must appear in staging history.
	out := mustRunOut(t, dir, "git", "log", "--format=%s", "aios/staging")
	if !strings.Contains(out, "T1") {
		t.Errorf("staging log missing T1:\n%s", out)
	}
	if !strings.Contains(out, "T2") {
		t.Errorf("staging log missing T2:\n%s", out)
	}
}

// TestRebaseReviewRejects: when ReReview returns (false, nil), the rebased
// task must be blocked and its commit must NOT appear in staging.
func TestRebaseReviewRejects(t *testing.T) {
	dir, origHead, applyT1, applyT2 := setupRebaseReviewRepo(t)

	reReview := func(newDiff []byte) (bool, error) {
		return false, nil
	}

	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
	}
	applyFns := map[string]func(){"T1": applyT1, "T2": applyT2}
	taskFn := buildTaskFn(t, dir, origHead, applyFns, reReview)

	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	// T1 must converge, T2 must be blocked.
	if len(rep.Converged) != 1 {
		t.Errorf("expected 1 converged, got %v", rep.Converged)
	}
	if len(rep.Blocked) != 1 {
		t.Errorf("expected 1 blocked, got %v", rep.Blocked)
	}
	if _, blocked := rep.Blocked["T2"]; !blocked {
		t.Errorf("expected T2 to be blocked, blocked map = %v", rep.Blocked)
	}

	// T1's commit must be in staging; T2's must NOT.
	stagingLog := mustRunOut(t, dir, "git", "log", "--format=%s", "aios/staging")
	if !strings.Contains(stagingLog, "T1") {
		t.Errorf("staging log should contain T1:\n%s", stagingLog)
	}
	if strings.Contains(stagingLog, "T2") {
		t.Errorf("staging log must NOT contain T2 after rejection:\n%s", stagingLog)
	}
}

// TestRebaseReviewNotCalledWhenDiffUnchanged: when T1 and T2 edit completely
// different files, T2's rebased diff is byte-for-byte identical to its
// original diff (no hunk-header shift). ReReview must NOT be called.
// Both tasks must converge.
func TestRebaseReviewNotCalledWhenDiffUnchanged(t *testing.T) {
	dir, origHead, applyT1, applyT2 := setupNoChangeRepo(t)

	var reReviewCalls atomic.Int32
	reReview := func(newDiff []byte) (bool, error) {
		reReviewCalls.Add(1)
		return true, nil
	}

	tasks := []*spec.Task{
		{ID: "T1", Status: "pending", Acceptance: []string{"x"}},
		{ID: "T2", Status: "pending", Acceptance: []string{"x"}},
	}
	applyFns := map[string]func(){"T1": applyT1, "T2": applyT2}
	taskFn := buildTaskFn(t, dir, origHead, applyFns, reReview)

	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          taskFn,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(rep.Blocked) != 0 {
		t.Errorf("expected no blocked tasks, got %v", rep.Blocked)
	}
	if len(rep.Converged) != 2 {
		t.Errorf("expected 2 converged, got %v", rep.Converged)
	}
	if got := reReviewCalls.Load(); got != 0 {
		t.Errorf("ReReview must NOT be called when diff is unchanged, got %d calls", got)
	}
}
