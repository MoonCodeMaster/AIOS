package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
)

// transientFailEngine fails with a transient error for the first failCount
// calls, then delegates to an inner FakeEngine. It wraps the inner engine
// with a RetryPolicy so the retry layer handles the transient failures
// transparently.
type transientFailEngine struct {
	inner     *engine.FakeEngine
	failCount int
	calls     int
}

func (e *transientFailEngine) Name() string { return e.inner.Name() }

func (e *transientFailEngine) Invoke(ctx context.Context, req engine.InvokeRequest) (*engine.InvokeResponse, error) {
	policy := engine.RetryPolicy{MaxAttempts: e.failCount + 1, BaseMs: 10, Enabled: true}
	attempt := 0
	resp, attempts, err := engine.WithRetry(ctx, policy, func() (*engine.InvokeResponse, error) {
		attempt++
		if attempt <= e.failCount {
			return nil, fmt.Errorf("claude exec: %w (stderr: 429 Too Many Requests)", fmt.Errorf("exit status 1"))
		}
		return e.inner.Invoke(ctx, req)
	})
	if resp != nil {
		resp.Attempts = attempts
	}
	return resp, err
}

func TestEngineRetry_OrchestratorRoundCompletes(t *testing.T) {
	approve := `{"approved":true,"criteria":[{"id":"c1","status":"satisfied"}],"issues":[]}`

	coder := &transientFailEngine{
		inner:     &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{{Text: "coded"}}},
		failCount: 2,
	}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: approve}}}

	task := &spec.Task{ID: "retry-test", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	dep := &orchestrator.Deps{
		Coder:     coder,
		Reviewer:  reviewer,
		Verifier:  stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		Diff:      func() (string, error) { return "diff", nil },
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}

	out, err := orchestrator.Run(context.Background(), task, dep)
	if err != nil {
		t.Fatalf("orchestrator.Run: %v", err)
	}
	if out.Final != orchestrator.StateConverged {
		t.Fatalf("final = %s, want converged", out.Final)
	}
	if len(out.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1", len(out.Rounds))
	}

	// The coder had 2 transient failures + 1 success = 2 recorded attempts.
	r := out.Rounds[0]
	if len(r.CoderAttempts) != 2 {
		t.Errorf("CoderAttempts = %d, want 2", len(r.CoderAttempts))
	}

	// Reviewer had no retries.
	if len(r.ReviewerAttempts) != 0 {
		t.Errorf("ReviewerAttempts = %d, want 0", len(r.ReviewerAttempts))
	}

	// Stall counter should not have incremented — only 1 round needed.
	if out.Final != orchestrator.StateConverged {
		t.Errorf("task should have converged, not stalled")
	}
}
