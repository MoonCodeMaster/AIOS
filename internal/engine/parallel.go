package engine

import (
	"context"
	"sync"
	"time"
)

// ParallelResult holds the outcome of one engine invocation in a parallel pair.
type ParallelResult struct {
	Engine     string          // engine name ("claude" or "codex")
	Response   *InvokeResponse // nil on error
	Err        error
	DurationMs int64 // wall-clock time for this engine's Invoke
}

// InvokeParallel runs two engine invocations concurrently with independent
// requests and returns both results. Both calls share the parent context: if
// the parent is cancelled, both are interrupted. Neither failure cancels the
// other — both results are always returned.
//
// Pass the same InvokeRequest twice when both engines should run identical
// prompts (e.g. stuck-task decomposition, PR review). Pass different requests
// when prompts or workdirs differ per engine (e.g. duel mode).
//
// Use cases: spec drafting, stuck-task decomposition, PR review, duel mode.
func InvokeParallel(ctx context.Context, a, b Engine, reqA, reqB InvokeRequest) (ra, rb ParallelResult) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		t0 := time.Now()
		resp, err := a.Invoke(ctx, reqA)
		ra = ParallelResult{Engine: a.Name(), Response: resp, Err: err, DurationMs: time.Since(t0).Milliseconds()}
	}()
	go func() {
		defer wg.Done()
		t0 := time.Now()
		resp, err := b.Invoke(ctx, reqB)
		rb = ParallelResult{Engine: b.Name(), Response: resp, Err: err, DurationMs: time.Since(t0).Milliseconds()}
	}()
	wg.Wait()
	return ra, rb
}

// InvokeRace runs two engine invocations concurrently and returns the first
// successful result. The losing engine's context is cancelled immediately.
// If both fail, both errors are returned via the results.
//
// Use case: duel/coder race mode where only the winner matters.
func InvokeRace(ctx context.Context, a, b Engine, reqA, reqB InvokeRequest) (winner ParallelResult, loser ParallelResult) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan ParallelResult, 2)

	go func() {
		t0 := time.Now()
		resp, err := a.Invoke(ctx, reqA)
		ch <- ParallelResult{Engine: a.Name(), Response: resp, Err: err, DurationMs: time.Since(t0).Milliseconds()}
	}()
	go func() {
		t0 := time.Now()
		resp, err := b.Invoke(ctx, reqB)
		ch <- ParallelResult{Engine: b.Name(), Response: resp, Err: err, DurationMs: time.Since(t0).Milliseconds()}
	}()

	first := <-ch
	if first.Err == nil {
		cancel() // cancel the loser
		second := <-ch
		return first, second
	}
	// First failed — wait for second.
	second := <-ch
	if second.Err == nil {
		return second, first
	}
	// Both failed — return in arrival order.
	return first, second
}
