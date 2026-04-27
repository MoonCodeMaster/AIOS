package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"
)

// RetryPolicy controls retry behavior for engine invocations.
type RetryPolicy struct {
	MaxAttempts int  // total attempts including the first; 1 = no retries
	BaseMs      int  // base backoff in milliseconds before jitter
	Enabled     bool // false disables retries entirely
}

// DefaultRetryPolicy returns the default retry policy: 3 attempts with 1s base.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseMs: 1000, Enabled: true}
}

// Attempt records a single failed invocation for the audit trail.
type Attempt struct {
	Attempt    int    `json:"attempt"`
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
}

// WithRetry calls fn up to policy.MaxAttempts times, backing off between
// transient failures. Returns the successful response, a slice of failed
// attempts (empty on first-try success), and any final error.
func WithRetry(ctx context.Context, policy RetryPolicy, fn func() (*InvokeResponse, error)) (*InvokeResponse, []Attempt, error) {
	if !policy.Enabled || policy.MaxAttempts <= 1 {
		start := time.Now()
		resp, err := fn()
		if err != nil {
			return nil, []Attempt{{Attempt: 1, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}}, err
		}
		return resp, nil, nil
	}

	var attempts []Attempt
	for i := 1; i <= policy.MaxAttempts; i++ {
		start := time.Now()
		resp, err := fn()
		dur := time.Since(start).Milliseconds()
		if err == nil {
			return resp, attempts, nil
		}

		attempts = append(attempts, Attempt{Attempt: i, Error: err.Error(), DurationMs: dur})

		if !classifyErr(err) {
			return nil, attempts, err
		}
		if i == policy.MaxAttempts {
			return nil, attempts, fmt.Errorf("engine invoke failed after %d attempts: %w", i, err)
		}

		// Backoff: attempt 2 = base, attempt 3 = base*4, etc.
		backoff := time.Duration(policy.BaseMs) * time.Millisecond
		if i > 1 {
			backoff *= time.Duration(1 << (2 * (i - 1)))
		}
		// Jitter ±25%
		jitter := 0.75 + rand.Float64()*0.5
		wait := time.Duration(float64(backoff) * jitter)

		select {
		case <-ctx.Done():
			return nil, attempts, ctx.Err()
		case <-time.After(wait):
		}
	}
	// unreachable
	return nil, attempts, fmt.Errorf("engine invoke: retry loop exited unexpectedly")
}

var transientPatterns = regexp.MustCompile(
	`(?i)(rate limit|429|5\d\d|connection reset|EOF|timeout)`,
)

var permanentPatterns = regexp.MustCompile(
	`(?i)(auth|forbidden|not found|invalid|permission denied|command not found)`,
)

// classifyErr returns true when err looks transient and the call should be
// retried. Returns false (permanent) for auth errors, context cancellation,
// and unknown errors — fail closed so we don't burn retries on unrecoverable
// problems.
func classifyErr(err error) bool {
	if err == nil {
		return false
	}
	// Context errors are never transient.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	msg := err.Error()

	// Empty stderr with non-zero exit = CLI crashed before output.
	if strings.Contains(msg, "(stderr: )") {
		return true
	}

	// JSON parse failure = CLI partially wrote output.
	if strings.Contains(msg, "output parse:") {
		return true
	}

	// Permanent patterns take priority over transient.
	if permanentPatterns.MatchString(msg) {
		return false
	}

	if transientPatterns.MatchString(msg) {
		return true
	}

	// Unknown → permanent (fail closed).
	return false
}
