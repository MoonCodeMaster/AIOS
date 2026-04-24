package integration

import (
	"context"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
)

func TestRunBudgetCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rb := orchestrator.NewRunBudget(ctx, cancel, 100)

	if err := rb.Add(60); err != nil {
		t.Fatalf("Add(60): %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("ctx cancelled prematurely")
	}
	if err := rb.Add(50); err != orchestrator.ErrRunBudgetExceeded {
		t.Fatalf("Add over cap: got %v, want ErrRunBudgetExceeded", err)
	}
	if ctx.Err() == nil {
		t.Errorf("ctx should be cancelled after overflow")
	}
}
