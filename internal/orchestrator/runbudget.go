package orchestrator

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrRunBudgetExceeded is returned by RunBudget.Add when the cap is hit.
var ErrRunBudgetExceeded = errors.New("run-level token budget exceeded")

// RunBudget is the run-wide (cross-task) token cap. The first Add that pushes
// the total above cap cancels the parent context so all workers wind down.
type RunBudget struct {
	cap     int64
	used    atomic.Int64
	cancel  context.CancelFunc
	tripped atomic.Bool
}

func NewRunBudget(ctx context.Context, cancel context.CancelFunc, cap int) *RunBudget {
	return &RunBudget{cap: int64(cap), cancel: cancel}
}

// Add records `tokens` of usage. Returns ErrRunBudgetExceeded the first time
// the cumulative total crosses cap; subsequent calls return the same error.
func (b *RunBudget) Add(tokens int) error {
	new := b.used.Add(int64(tokens))
	if new > b.cap {
		if b.tripped.CompareAndSwap(false, true) {
			b.cancel()
		}
		return ErrRunBudgetExceeded
	}
	return nil
}

func (b *RunBudget) Used() int64 { return b.used.Load() }
func (b *RunBudget) Cap() int64  { return b.cap }
