package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// Two independent branches: A→C and B→D (no edges between branches).
// With workers=2, A and B should run concurrently.
func TestRunAllParallelTwoBranches(t *testing.T) {
	dir := seedRepo(t)
	tasks := []*spec.Task{
		{ID: "A", Status: "pending", Acceptance: []string{"x"}},
		{ID: "B", Status: "pending", Acceptance: []string{"x"}},
		{ID: "C", Status: "pending", Acceptance: []string{"x"}, DependsOn: []string{"A"}},
		{ID: "D", Status: "pending", Acceptance: []string{"x"}, DependsOn: []string{"B"}},
	}
	var inflight, peak atomic.Int32
	var gitMu sync.Mutex
	work := func(ctx context.Context, id orchestrator.TaskID) orchestrator.TaskResult {
		now := inflight.Add(1)
		for {
			cur := peak.Load()
			if now <= cur || peak.CompareAndSwap(cur, now) {
				break
			}
		}
		// Serialize git operations: the working tree is shared across goroutines.
		gitMu.Lock()
		commitTaskBranch(t, dir, string(id))
		gitMu.Unlock()
		// Sleep after releasing the mutex so multiple goroutines are inflight
		// simultaneously — this is the parallelism observation window.
		time.Sleep(50 * time.Millisecond)
		inflight.Add(-1)
		return orchestrator.TaskResult{ID: id, Status: "converged"}
	}
	rep, err := orchestrator.RunAll(context.Background(), orchestrator.RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       2,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Work:          work,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(rep.Converged) != 4 {
		t.Errorf("Converged = %d, want 4 (got %v / blocked %v)", len(rep.Converged), rep.Converged, rep.Blocked)
	}
	if peak.Load() < 2 {
		t.Errorf("peak inflight = %d, want >= 2 (parallelism not observed)", peak.Load())
	}
}
