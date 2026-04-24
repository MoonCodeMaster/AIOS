package orchestrator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunBudgetUnderCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rb := NewRunBudget(ctx, cancel, 1000)
	if err := rb.Add(500); err != nil {
		t.Fatalf("Add(500): %v", err)
	}
	if err := rb.Add(499); err != nil {
		t.Fatalf("Add(499): %v", err)
	}
	if rb.Used() != 999 {
		t.Errorf("Used = %d, want 999", rb.Used())
	}
	if ctx.Err() != nil {
		t.Errorf("ctx unexpectedly cancelled: %v", ctx.Err())
	}
}

func TestRunBudgetOverCapCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rb := NewRunBudget(ctx, cancel, 1000)
	if err := rb.Add(700); err != nil {
		t.Fatal(err)
	}
	err := rb.Add(400)
	if err != ErrRunBudgetExceeded {
		t.Fatalf("Add over cap returned %v, want ErrRunBudgetExceeded", err)
	}
	if ctx.Err() == nil {
		t.Errorf("ctx should be cancelled after overflow")
	}
}

func TestRunBudgetConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rb := NewRunBudget(ctx, cancel, 100_000)
	var wg sync.WaitGroup
	var errs atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rb.Add(500); err != nil {
				errs.Add(1)
			}
		}()
	}
	wg.Wait()
	if rb.Used() != 50_000 {
		t.Errorf("Used = %d, want 50000", rb.Used())
	}
	if errs.Load() != 0 {
		t.Errorf("got %d concurrent Add errors, want 0", errs.Load())
	}
}
