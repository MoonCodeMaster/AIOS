package integration

import (
	"context"
	"testing"
	"time"

	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/orchestrator"
	"github.com/Solaxis/aios/internal/spec"
	"github.com/Solaxis/aios/internal/verify"
)

func TestRunBlocked_MaxRoundsExceeded(t *testing.T) {
	reject := `{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[]}`

	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: reject}, {Text: reject}, {Text: reject}}}

	task := &spec.Task{ID: "x", Kind: "feature", Acceptance: []string{"c1"}}
	dep := &orchestrator.Deps{Coder: coder, Reviewer: reviewer,
		Verifier: stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		MaxRounds: 3, MaxTokens: 1_000_000, MaxWall: time.Minute}
	out, err := orchestrator.Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != orchestrator.StateBlocked || out.Reason != "max_rounds_exceeded" {
		t.Errorf("final=%s reason=%s", out.Final, out.Reason)
	}
}
