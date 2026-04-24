package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/spec"
	"github.com/Solaxis/aios/internal/verify"
)

// reviewerApproveJSON is a canned reviewer response in the structured format
// documented in spec §5.3.
const reviewerApproveJSON = `{"approved":true,"criteria":[{"id":"c1","status":"satisfied"}],"issues":[]}`

func approvedReviewer() *engine.FakeEngine {
	return &engine.FakeEngine{
		Name_:  "codex",
		Script: []engine.InvokeResponse{{Text: reviewerApproveJSON, UsageTokens: 50}},
	}
}

func TestRun_HappyPath(t *testing.T) {
	coder := &engine.FakeEngine{
		Name_:  "claude",
		Script: []engine.InvokeResponse{{Text: "coded", UsageTokens: 100}},
	}
	reviewer := approvedReviewer()

	task := &spec.Task{
		ID:         "001-a",
		Kind:       "feature",
		Status:     "pending",
		Acceptance: []string{"c1"},
	}

	dep := &Deps{
		Coder:       coder,
		Reviewer:    reviewer,
		Verifier:    &stubVerifier{results: []verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		MaxRounds:   5,
		MaxTokens:   10000,
		MaxWall:     time.Minute,
	}

	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateConverged {
		t.Errorf("final = %s", outcome.Final)
	}
	if len(outcome.Rounds) != 1 {
		t.Errorf("rounds = %d", len(outcome.Rounds))
	}
}

type stubVerifier struct {
	results []verify.CheckResult
}

func (s *stubVerifier) Run() []verify.CheckResult { return s.results }

func TestRun_RejectThenConverge(t *testing.T) {
	rejectJSON := `{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"fix c1"}]}`
	approveJSON := reviewerApproveJSON

	coder := &engine.FakeEngine{
		Name_: "claude",
		Script: []engine.InvokeResponse{
			{Text: "round1 code"}, {Text: "round2 code"},
		},
	}
	reviewer := &engine.FakeEngine{
		Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: rejectJSON}, {Text: approveJSON},
		},
	}

	task := &spec.Task{ID: "x", Kind: "feature", Status: "pending",
		Acceptance: []string{"c1"}}

	dep := &Deps{
		Coder:    coder,
		Reviewer: reviewer,
		Verifier: &stubVerifier{results: []verify.CheckResult{{Name: "t", Status: verify.StatusPassed}}},
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}
	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateConverged {
		t.Errorf("final = %s", outcome.Final)
	}
	if len(outcome.Rounds) != 2 {
		t.Errorf("rounds = %d", len(outcome.Rounds))
	}
	// coder should have received exactly 2 calls; second one is a revision.
	if len(coder.Received) != 2 {
		t.Errorf("coder calls = %d", len(coder.Received))
	}
}

func TestRun_MaxRounds_Blocked(t *testing.T) {
	rejectJSON := `{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"keep failing"}]}`

	coder := &engine.FakeEngine{Name_: "claude",
		Script: make([]engine.InvokeResponse, 3)}
	for i := range coder.Script {
		coder.Script[i] = engine.InvokeResponse{Text: "code"}
	}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: make([]engine.InvokeResponse, 3)}
	for i := range reviewer.Script {
		reviewer.Script[i] = engine.InvokeResponse{Text: rejectJSON}
	}

	task := &spec.Task{ID: "x", Kind: "feature", Status: "pending",
		Acceptance: []string{"c1"}}

	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier:  &stubVerifier{results: []verify.CheckResult{{Name: "t", Status: verify.StatusPassed}}},
		MaxRounds: 3, MaxTokens: 100000, MaxWall: time.Minute,
	}
	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateBlocked {
		t.Errorf("final = %s", outcome.Final)
	}
	if outcome.Reason != "max_rounds_exceeded" {
		t.Errorf("reason = %q", outcome.Reason)
	}
}

func TestRun_MaxTokens_Blocked(t *testing.T) {
	rejectJSON := `{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[]}`

	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "a", UsageTokens: 5000}, {Text: "b", UsageTokens: 5000}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: rejectJSON, UsageTokens: 5000}}}

	task := &spec.Task{ID: "x", Kind: "feature", Acceptance: []string{"c1"}}
	dep := &Deps{Coder: coder, Reviewer: reviewer,
		Verifier:  &stubVerifier{results: []verify.CheckResult{{Name: "t", Status: verify.StatusPassed}}},
		MaxRounds: 10, MaxTokens: 12000, MaxWall: time.Minute}
	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateBlocked {
		t.Errorf("final = %s", outcome.Final)
	}
	if outcome.Reason != "max_tokens_exceeded" {
		t.Errorf("reason = %q", outcome.Reason)
	}
}

// TestRunAllRoutesMergeRequest verifies that when a Task callback returns a
// non-nil *MergeRequest, RunAll submits it to the MergeQueue and rewrites the
// TaskResult based on the merge outcome.
func TestRunAllRoutesMergeRequest(t *testing.T) {
	dir := gitInit(t)

	// Set up a task branch ready to FF into staging.
	mustGit(t, dir, "checkout", "-q", "-b", "aios/task/T1", "aios/staging")
	mustWrite(t, filepath.Join(dir, "t1.txt"), "hello\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "T1")
	parentSHA := gitSHA(t, dir, "aios/staging")
	mustGit(t, dir, "checkout", "-q", "aios/staging")

	tasks := []*spec.Task{tk("T1")}

	task := func(ctx context.Context, id TaskID) (TaskResult, *MergeRequest) {
		// Just return the merge request — no git operations here. The branch
		// was set up above; RunAll must do the merge via MergeQueue.
		return TaskResult{ID: id, Status: "converged"}, &MergeRequest{
			TaskID:    id,
			Branch:    "aios/task/" + string(id),
			ParentSHA: parentSHA,
			Diff:      nil,
		}
	}

	report, err := RunAll(context.Background(), RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Task:          task,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(report.Converged) != 1 || report.Converged[0] != "T1" {
		t.Errorf("Converged = %v, want [T1]", report.Converged)
	}
	// Staging must have advanced (FF merged).
	if gitSHA(t, dir, "aios/staging") == parentSHA {
		t.Errorf("staging SHA unchanged — merge was not performed")
	}
}

func TestRunAllSerialN1(t *testing.T) {
	dir := gitInit(t)
	tasks := []*spec.Task{tk("T1")}

	work := func(ctx context.Context, id TaskID) TaskResult {
		// Simulate a task that lands on its own task branch.
		mustGit(t, dir, "checkout", "-q", "-b", "aios/task/"+string(id), "aios/staging")
		mustWrite(t, filepath.Join(dir, string(id)+".txt"), "ok\n")
		mustGit(t, dir, "add", ".")
		mustGit(t, dir, "commit", "-q", "-m", string(id))
		return TaskResult{ID: id, Status: "converged"}
	}

	report, err := RunAll(context.Background(), RunAllOpts{
		RepoDir:       dir,
		StagingBranch: "aios/staging",
		Tasks:         tasks,
		Workers:       1,
		Reviewer:      &engine.FakeEngine{Name_: "rev"},
		Work:          work,
	})
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(report.Converged) != 1 || report.Converged[0] != "T1" {
		t.Errorf("Converged = %v, want [T1]", report.Converged)
	}
}
