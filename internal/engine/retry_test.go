package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestWithRetry_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	policy := DefaultRetryPolicy()
	resp, attempts, err := WithRetry(context.Background(), policy, func() (*InvokeResponse, error) {
		calls++
		return &InvokeResponse{Text: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if len(attempts) != 0 {
		t.Errorf("attempts = %d, want 0 (no failures to record)", len(attempts))
	}
	if resp.Text != "ok" {
		t.Errorf("resp.Text = %q, want ok", resp.Text)
	}
}

func TestWithRetry_SucceedsOnSecondAttempt(t *testing.T) {
	calls := 0
	policy := RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true}
	resp, attempts, err := WithRetry(context.Background(), policy, func() (*InvokeResponse, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("claude exec: %w (stderr: rate limit exceeded)", errors.New("exit status 1"))
		}
		return &InvokeResponse{Text: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(attempts) != 1 {
		t.Errorf("attempts = %d, want 1", len(attempts))
	}
	if resp.Text != "ok" {
		t.Errorf("resp.Text = %q, want ok", resp.Text)
	}
}

func TestWithRetry_ExhaustsAttempts(t *testing.T) {
	calls := 0
	policy := RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true}
	_, attempts, err := WithRetry(context.Background(), policy, func() (*InvokeResponse, error) {
		calls++
		return nil, fmt.Errorf("claude exec: %w (stderr: rate limit exceeded)", errors.New("exit status 1"))
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if len(attempts) != 3 {
		t.Errorf("attempts = %d, want 3", len(attempts))
	}
}

func TestWithRetry_RespectsCtxDoneMidBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	policy := RetryPolicy{MaxAttempts: 3, BaseMs: 5000, Enabled: true}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, _, err := WithRetry(ctx, policy, func() (*InvokeResponse, error) {
		calls++
		return nil, fmt.Errorf("claude exec: %w (stderr: rate limit exceeded)", errors.New("exit status 1"))
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls > 1 {
		t.Errorf("calls = %d, want 1 (should not retry after ctx cancel)", calls)
	}
}

func TestWithRetry_DisabledPolicy(t *testing.T) {
	calls := 0
	policy := RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: false}
	_, _, err := WithRetry(context.Background(), policy, func() (*InvokeResponse, error) {
		calls++
		return nil, fmt.Errorf("claude exec: %w (stderr: rate limit exceeded)", errors.New("exit status 1"))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (retry disabled)", calls)
	}
}

func TestWithRetry_PermanentErrorNoRetry(t *testing.T) {
	calls := 0
	policy := RetryPolicy{MaxAttempts: 3, BaseMs: 10, Enabled: true}
	_, _, err := WithRetry(context.Background(), policy, func() (*InvokeResponse, error) {
		calls++
		return nil, fmt.Errorf("claude exec: %w (stderr: auth token expired)", errors.New("exit status 1"))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (permanent error should not retry)", calls)
	}
}

func TestClassifyErr(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		// Transient patterns
		{"rate limit", fmt.Errorf("exec: exit 1 (stderr: rate limit exceeded)"), true},
		{"429 status", fmt.Errorf("exec: exit 1 (stderr: 429 Too Many Requests)"), true},
		{"500 status", fmt.Errorf("exec: exit 1 (stderr: 500 Internal Server Error)"), true},
		{"502 status", fmt.Errorf("exec: exit 1 (stderr: 502 Bad Gateway)"), true},
		{"503 status", fmt.Errorf("exec: exit 1 (stderr: 503 Service Unavailable)"), true},
		{"connection reset", fmt.Errorf("exec: exit 1 (stderr: connection reset by peer)"), true},
		{"EOF", fmt.Errorf("exec: exit 1 (stderr: unexpected EOF)"), true},
		{"timeout", fmt.Errorf("exec: exit 1 (stderr: timeout waiting for response)"), true},
		{"empty stdout crash", fmt.Errorf("exec: exit 1 (stderr: )"), true},
		{"json parse on non-empty", fmt.Errorf("claude output parse: invalid character"), true},
		// Permanent patterns
		{"auth error", fmt.Errorf("exec: exit 1 (stderr: auth token expired)"), false},
		{"forbidden", fmt.Errorf("exec: exit 1 (stderr: forbidden)"), false},
		{"not found", fmt.Errorf("exec: exit 1 (stderr: not found)"), false},
		{"invalid request", fmt.Errorf("exec: exit 1 (stderr: invalid request body)"), false},
		{"permission denied", fmt.Errorf("exec: exit 1 (stderr: permission denied)"), false},
		{"command not found", fmt.Errorf("exec: exit 1 (stderr: command not found)"), false},
		// Unknown → permanent (fail closed)
		{"unknown error", fmt.Errorf("something unexpected"), false},
		// Context errors → permanent (propagate immediately)
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"wrapped context canceled", fmt.Errorf("invoke: %w", context.Canceled), false},
		{"wrapped deadline exceeded", fmt.Errorf("invoke: %w", context.DeadlineExceeded), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyErr(tt.err)
			if got != tt.transient {
				t.Errorf("classifyErr(%q) = %v, want %v", tt.err, got, tt.transient)
			}
		})
	}
}
