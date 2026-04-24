package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

func TestPoolBoundedConcurrency(t *testing.T) {
	const N = 3
	const total = 10
	tasks := make([]*spec.Task, total)
	for i := 0; i < total; i++ {
		tasks[i] = tk(string(rune('a' + i)))
	}
	sched, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	var inflight, peak atomic.Int32
	work := func(ctx context.Context, id TaskID) TaskResult {
		now := inflight.Add(1)
		for {
			cur := peak.Load()
			if now <= cur || peak.CompareAndSwap(cur, now) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inflight.Add(-1)
		return TaskResult{ID: id, Status: "converged"}
	}
	pool := NewPool(N, sched, work)
	if err := pool.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if int(peak.Load()) > N {
		t.Errorf("peak inflight = %d, want <= %d", peak.Load(), N)
	}
}

func TestPoolCancellationStopsWorkers(t *testing.T) {
	tasks := []*spec.Task{tk("a"), tk("b"), tk("c")}
	sched, err := NewScheduler(tasks)
	if err != nil {
		t.Fatal(err)
	}
	work := func(ctx context.Context, id TaskID) TaskResult {
		select {
		case <-ctx.Done():
			return TaskResult{ID: id, Status: "blocked", Reason: "ctx-cancelled"}
		case <-time.After(5 * time.Second):
			return TaskResult{ID: id, Status: "converged"}
		}
	}
	pool := NewPool(2, sched, work)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := pool.Run(ctx); err != nil && err != context.DeadlineExceeded {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Run took %v; should have honored ctx cancel", elapsed)
	}
}
