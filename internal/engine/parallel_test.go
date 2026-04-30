package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// delayEngine is a test Engine that sleeps before responding.
type delayEngine struct {
	name     string
	delay    time.Duration
	resp     InvokeResponse
	err      error
	invoked  atomic.Int32
	ctxDone  atomic.Bool // set to true if ctx was cancelled during invoke
}

func (d *delayEngine) Name() string { return d.name }

func (d *delayEngine) Invoke(ctx context.Context, _ InvokeRequest) (*InvokeResponse, error) {
	d.invoked.Add(1)
	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		d.ctxDone.Store(true)
		return nil, ctx.Err()
	}
	if d.err != nil {
		return nil, d.err
	}
	r := d.resp
	return &r, nil
}

func TestInvokeParallel_BothSucceed(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 10 * time.Millisecond, resp: InvokeResponse{Text: "A"}}
	b := &delayEngine{name: "codex", delay: 10 * time.Millisecond, resp: InvokeResponse{Text: "B"}}

	ra, rb := InvokeParallel(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})

	if ra.Err != nil {
		t.Errorf("ra.Err = %v", ra.Err)
	}
	if rb.Err != nil {
		t.Errorf("rb.Err = %v", rb.Err)
	}
	if ra.Response.Text != "A" {
		t.Errorf("ra.Response.Text = %q, want A", ra.Response.Text)
	}
	if rb.Response.Text != "B" {
		t.Errorf("rb.Response.Text = %q, want B", rb.Response.Text)
	}
	if ra.Engine != "claude" {
		t.Errorf("ra.Engine = %q, want claude", ra.Engine)
	}
	if rb.Engine != "codex" {
		t.Errorf("rb.Engine = %q, want codex", rb.Engine)
	}
}

func TestInvokeParallel_OneFailsOtherSucceeds(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 10 * time.Millisecond, err: errors.New("rate limit")}
	b := &delayEngine{name: "codex", delay: 10 * time.Millisecond, resp: InvokeResponse{Text: "B"}}

	ra, rb := InvokeParallel(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})

	if ra.Err == nil {
		t.Error("ra.Err should be non-nil")
	}
	if rb.Err != nil {
		t.Errorf("rb.Err = %v, want nil", rb.Err)
	}
	if rb.Response.Text != "B" {
		t.Errorf("rb.Response.Text = %q, want B", rb.Response.Text)
	}
}

func TestInvokeParallel_ContextCancel(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 5 * time.Second, resp: InvokeResponse{Text: "A"}}
	b := &delayEngine{name: "codex", delay: 5 * time.Second, resp: InvokeResponse{Text: "B"}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	ra, rb := InvokeParallel(ctx, a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("InvokeParallel took %s, expected <2s (cancel should interrupt)", elapsed)
	}
	if ra.Err == nil {
		t.Error("ra.Err should be non-nil after cancel")
	}
	if rb.Err == nil {
		t.Error("rb.Err should be non-nil after cancel")
	}
}

func TestInvokeParallel_RunsConcurrently(t *testing.T) {
	t.Parallel()
	// Both engines take 50ms. If run sequentially, total would be >=100ms.
	// If concurrent, total should be ~50ms.
	a := &delayEngine{name: "claude", delay: 50 * time.Millisecond, resp: InvokeResponse{Text: "A"}}
	b := &delayEngine{name: "codex", delay: 50 * time.Millisecond, resp: InvokeResponse{Text: "B"}}

	start := time.Now()
	ra, rb := InvokeParallel(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})
	elapsed := time.Since(start)

	if ra.Err != nil || rb.Err != nil {
		t.Fatalf("unexpected errors: %v, %v", ra.Err, rb.Err)
	}
	if elapsed > 90*time.Millisecond {
		t.Errorf("InvokeParallel took %s, expected ~50ms (should be concurrent)", elapsed)
	}
}

func TestInvokeRace_FastWins(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 10 * time.Millisecond, resp: InvokeResponse{Text: "fast"}}
	b := &delayEngine{name: "codex", delay: 500 * time.Millisecond, resp: InvokeResponse{Text: "slow"}}

	winner, loser := InvokeRace(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})

	if winner.Engine != "claude" {
		t.Errorf("winner.Engine = %q, want claude", winner.Engine)
	}
	if winner.Response.Text != "fast" {
		t.Errorf("winner.Response.Text = %q, want fast", winner.Response.Text)
	}
	if winner.Err != nil {
		t.Errorf("winner.Err = %v", winner.Err)
	}
	// Loser should have been cancelled.
	if loser.Err == nil {
		t.Error("loser.Err should be non-nil (cancelled)")
	}
}

func TestInvokeRace_CancelsLoser(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 10 * time.Millisecond, resp: InvokeResponse{Text: "fast"}}
	b := &delayEngine{name: "codex", delay: 2 * time.Second, resp: InvokeResponse{Text: "slow"}}

	start := time.Now()
	_, _ = InvokeRace(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})
	elapsed := time.Since(start)

	// Should complete quickly — not wait for the slow engine.
	if elapsed > 1*time.Second {
		t.Errorf("InvokeRace took %s, expected <1s (loser should be cancelled)", elapsed)
	}
	// Verify the slow engine saw the cancellation.
	if !b.ctxDone.Load() {
		t.Error("slow engine should have observed context cancellation")
	}
}

func TestInvokeRace_FirstFailsSecondWins(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 10 * time.Millisecond, err: errors.New("crash")}
	b := &delayEngine{name: "codex", delay: 50 * time.Millisecond, resp: InvokeResponse{Text: "ok"}}

	winner, loser := InvokeRace(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})

	if winner.Engine != "codex" {
		t.Errorf("winner.Engine = %q, want codex", winner.Engine)
	}
	if winner.Response.Text != "ok" {
		t.Errorf("winner.Response.Text = %q, want ok", winner.Response.Text)
	}
	if loser.Engine != "claude" {
		t.Errorf("loser.Engine = %q, want claude", loser.Engine)
	}
	if loser.Err == nil {
		t.Error("loser.Err should be non-nil")
	}
}

func TestInvokeRace_BothFail(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 10 * time.Millisecond, err: errors.New("fail-a")}
	b := &delayEngine{name: "codex", delay: 20 * time.Millisecond, err: errors.New("fail-b")}

	winner, loser := InvokeRace(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})

	if winner.Err == nil {
		t.Error("winner.Err should be non-nil when both fail")
	}
	if loser.Err == nil {
		t.Error("loser.Err should be non-nil when both fail")
	}
}

func TestInvokeRace_ContextCancel(t *testing.T) {
	t.Parallel()
	a := &delayEngine{name: "claude", delay: 5 * time.Second, resp: InvokeResponse{Text: "A"}}
	b := &delayEngine{name: "codex", delay: 5 * time.Second, resp: InvokeResponse{Text: "B"}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	winner, loser := InvokeRace(ctx, a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("InvokeRace took %s, expected <2s", elapsed)
	}
	if winner.Err == nil && loser.Err == nil {
		t.Error("at least one should have errored from context cancel")
	}
}

func TestInvokeParallel_NoSharedMutableState(t *testing.T) {
	t.Parallel()
	// Run many parallel invocations concurrently to detect races.
	// This test is meaningful only with -race.
	a := &delayEngine{name: "claude", delay: 1 * time.Millisecond, resp: InvokeResponse{Text: "A"}}
	b := &delayEngine{name: "codex", delay: 1 * time.Millisecond, resp: InvokeResponse{Text: "B"}}

	done := make(chan struct{}, 100)
	for i := 0; i < 100; i++ {
		go func() {
			ra, rb := InvokeParallel(context.Background(), a, b, InvokeRequest{Prompt: "test"}, InvokeRequest{Prompt: "test"})
			if ra.Err != nil || rb.Err != nil {
				t.Errorf("unexpected error in concurrent run")
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for concurrent invocations")
		}
	}
	if a.invoked.Load() != 100 {
		t.Errorf("a.invoked = %d, want 100", a.invoked.Load())
	}
	if b.invoked.Load() != 100 {
		t.Errorf("b.invoked = %d, want 100", b.invoked.Load())
	}
}
