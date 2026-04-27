package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
)

func TestCompressHistory_SixRoundTask(t *testing.T) {
	reject := func(round int) string {
		return fmt.Sprintf(
			`{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","category":"correctness","note":"bug in round %d","file":"handler.go","line":%d}]}`,
			round, round*10,
		)
	}
	approve := `{"approved":true,"criteria":[{"id":"c1","status":"satisfied"}],"issues":[]}`

	// 6 rounds: reject 5 times, approve on round 6.
	var coderScript []engine.InvokeResponse
	var reviewerScript []engine.InvokeResponse
	for i := 1; i <= 5; i++ {
		coderScript = append(coderScript, engine.InvokeResponse{Text: fmt.Sprintf("fix round %d", i)})
		reviewerScript = append(reviewerScript, engine.InvokeResponse{Text: reject(i)})
	}
	coderScript = append(coderScript, engine.InvokeResponse{Text: "final fix"})
	reviewerScript = append(reviewerScript, engine.InvokeResponse{Text: approve})

	coder := &engine.FakeEngine{Name_: "claude", Script: coderScript}
	reviewer := &engine.FakeEngine{Name_: "codex", Script: reviewerScript}

	task := &spec.Task{ID: "compress-test", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	dep := &orchestrator.Deps{
		Coder:     coder,
		Reviewer:  reviewer,
		Verifier:  stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		Diff:      func() (string, error) { return "diff content", nil },
		MaxRounds: 10, MaxTokens: 500000, MaxWall: 5 * time.Minute,
		Compress: orchestrator.CompressConfig{
			Enabled:      true,
			AfterRounds:  2,
			TargetTokens: 50000,
		},
	}

	out, err := orchestrator.Run(context.Background(), task, dep)
	if err != nil {
		t.Fatalf("orchestrator.Run: %v", err)
	}
	if out.Final != orchestrator.StateConverged {
		t.Fatalf("final = %s, want converged (reason: %s)", out.Final, out.Reason)
	}
	if len(out.Rounds) != 6 {
		t.Fatalf("rounds = %d, want 6", len(out.Rounds))
	}

	// Round 4 (index 3) should have compression: rounds 1 is compressed
	// (rounds 2-3 are the last K=2 verbatim at the time round 4 renders).
	r4 := out.Rounds[3]
	if r4.CompressedPriorBrief == "" {
		t.Error("round 4 should have CompressedPriorBrief set")
	}
	if !strings.Contains(r4.CompressedPriorBrief, "Prior rounds (compressed):") {
		t.Error("round 4 brief should contain header")
	}
	// The coder prompt for round 4 should contain the compressed brief.
	if !strings.Contains(r4.CoderPrompt, "Prior rounds (compressed):") {
		t.Error("round 4 coder prompt should contain compressed brief")
	}
	// Round 1's blocking-issue note SHOULD appear in round 4's coder prompt:
	// without it, the coder forgets what the reviewer rejected and tends to
	// repeat the same mistake. The full per-round prose is gone (replaced
	// by a compressed line), but the note text itself survives.
	if !strings.Contains(r4.CoderPrompt, "bug in round 1") {
		t.Error("round 4 coder prompt should preserve round-1 blocking-issue note text")
	}

	// Rounds 1-2 should NOT have compression (below threshold).
	if out.Rounds[0].CompressedPriorBrief != "" {
		t.Error("round 1 should not have compression")
	}
	if out.Rounds[1].CompressedPriorBrief != "" {
		t.Error("round 2 should not have compression")
	}
}
