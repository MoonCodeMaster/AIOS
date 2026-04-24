package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
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

// seqVerifier returns a different result set on each successive Run() call,
// for tests that need verify state to evolve across rounds.
type seqVerifier struct {
	results [][]verify.CheckResult
	idx     int
}

func (s *seqVerifier) Run() []verify.CheckResult {
	r := s.results[s.idx]
	s.idx++
	return r
}

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
	// Rejections must be DISTINCT so stall detection (3 identical rounds in a
	// row) does not fire before max_rounds_exceeded. This test is about the
	// budget gate, not about stall.
	rejects := []string{
		`{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"r1"}]}`,
		`{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"r2"}]}`,
		`{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"r3"}]}`,
	}

	coder := &engine.FakeEngine{Name_: "claude",
		Script: make([]engine.InvokeResponse, 3)}
	for i := range coder.Script {
		coder.Script[i] = engine.InvokeResponse{Text: "code"}
	}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: make([]engine.InvokeResponse, 3)}
	for i := range reviewer.Script {
		reviewer.Script[i] = engine.InvokeResponse{Text: rejects[i]}
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
	if outcome.BlockReason == nil || outcome.BlockReason.Code != CodeMaxRoundsExceeded {
		t.Errorf("BlockReason = %+v, want Code=%s", outcome.BlockReason, CodeMaxRoundsExceeded)
	}
}

func TestRun_CoderInputCarriesPriorRound(t *testing.T) {
	// Two rounds: reject then approve. Round 2's CoderInput must include the
	// reviewer issues, prior diff, and prior verify results from round 1.
	rejectJSON := `{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"fix the loop"}]}`

	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "r1"}, {Text: "r2"}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: rejectJSON}, {Text: reviewerApproveJSON}}}

	diffs := []string{"diff-from-round-1", "diff-from-round-2"}
	var diffCalls int
	diffFn := func() (string, error) {
		d := diffs[diffCalls]
		diffCalls++
		return d, nil
	}

	// Round 1 verify fails (so we must revise); round 2 verify passes
	// (so the approve from round 2's reviewer can converge the loop).
	verifier := &seqVerifier{results: [][]verify.CheckResult{
		{{Name: "test_cmd", Status: verify.StatusFailed, ExitCode: 1}},
		{{Name: "test_cmd", Status: verify.StatusPassed}},
	}}
	var inputs []CoderInput
	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier:    verifier,
		Diff:        diffFn,
		RenderCoder: func(in CoderInput) string { inputs = append(inputs, in); return "stub" },
		MaxRounds:   5, MaxTokens: 100000, MaxWall: time.Minute,
	}
	task := &spec.Task{ID: "t", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	if _, err := Run(context.Background(), task, dep); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("RenderCoder called %d times, want 2", len(inputs))
	}
	// Round 1: clean slate.
	if inputs[0].IsRevision || inputs[0].Round != 1 ||
		len(inputs[0].Issues) != 0 || inputs[0].PrevDiff != "" || len(inputs[0].PrevChecks) != 0 {
		t.Errorf("round 1 input should be empty: %+v", inputs[0])
	}
	// Round 2: revision with full prior context.
	r2 := inputs[1]
	if !r2.IsRevision || r2.Round != 2 {
		t.Errorf("round 2 IsRevision=%v Round=%d", r2.IsRevision, r2.Round)
	}
	// Round 1 had both a reviewer-reported issue AND a red verify check, so
	// round 2's coder must see the reviewer's note plus the synthetic
	// verify-failure issue.
	var sawReviewer, sawVerify bool
	for _, i := range r2.Issues {
		if i.Note == "fix the loop" {
			sawReviewer = true
		}
		if strings.Contains(i.Note, "test_cmd") && strings.Contains(i.Note, "failed") {
			sawVerify = true
		}
	}
	if !sawReviewer || !sawVerify {
		t.Errorf("round 2 Issues missing reviewer (%v) or synthetic verify (%v); got %+v",
			sawReviewer, sawVerify, r2.Issues)
	}
	if r2.PrevDiff != "diff-from-round-1" {
		t.Errorf("round 2 PrevDiff = %q, want diff-from-round-1", r2.PrevDiff)
	}
	if len(r2.PrevChecks) != 1 || r2.PrevChecks[0].Status != verify.StatusFailed {
		t.Errorf("round 2 PrevChecks = %+v", r2.PrevChecks)
	}
}

func TestRun_StallDetected(t *testing.T) {
	// Three identical reviewer rejections in a row → block early as stall,
	// without burning the rest of the round budget.
	rejectJSON := `{"approved":false,"criteria":[{"id":"c1","status":"unmet"}],"issues":[{"severity":"blocking","note":"same problem"}]}`

	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "r1"}, {Text: "r2"}, {Text: "r3"}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: rejectJSON}, {Text: rejectJSON}, {Text: rejectJSON}}}

	task := &spec.Task{ID: "x", Kind: "feature", Status: "pending",
		Acceptance: []string{"c1"}}

	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier: &stubVerifier{results: []verify.CheckResult{{Name: "t", Status: verify.StatusPassed}}},
		// Generous budget — stall should fire well before max_rounds.
		MaxRounds: 10, MaxTokens: 1_000_000, MaxWall: time.Minute,
	}
	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateBlocked {
		t.Fatalf("final = %s", outcome.Final)
	}
	if !strings.HasPrefix(outcome.Reason, "stall_no_progress:") {
		t.Errorf("reason = %q, want stall_no_progress prefix", outcome.Reason)
	}
	if outcome.BlockReason == nil || outcome.BlockReason.Code != CodeStallNoProgress {
		t.Errorf("BlockReason = %+v, want Code=%s", outcome.BlockReason, CodeStallNoProgress)
	}
	if len(outcome.Rounds) != 3 {
		t.Errorf("rounds = %d, want 3 (stall threshold)", len(outcome.Rounds))
	}
}

func TestIssueFingerprint_OrderInsensitive(t *testing.T) {
	a := ReviewResult{
		Criteria: []CriterionStatus{
			{ID: "c1", Status: "unmet"},
			{ID: "c2", Status: "satisfied"},
		},
		Issues: []ReviewIssue{
			{Severity: "blocking", Note: "x"},
			{Severity: "nit", Note: "y"},
		},
	}
	b := ReviewResult{
		Criteria: []CriterionStatus{
			{ID: "c2", Status: "satisfied"}, // reordered
			{ID: "c1", Status: "unmet"},
		},
		Issues: []ReviewIssue{
			{Severity: "nit", Note: "y"}, // reordered
			{Severity: "blocking", Note: "x"},
		},
	}
	if issueFingerprint(a) != issueFingerprint(b) {
		t.Errorf("fingerprints differ on reorder; got %q vs %q",
			issueFingerprint(a), issueFingerprint(b))
	}
	// All criteria satisfied and no issues → empty fingerprint (won't trip stall).
	c := ReviewResult{Criteria: []CriterionStatus{{ID: "c1", Status: "satisfied"}}}
	if issueFingerprint(c) != "" {
		t.Errorf("clean review fingerprint = %q, want empty", issueFingerprint(c))
	}
}

func TestRun_PropagatesWorkdir(t *testing.T) {
	coder := &engine.FakeEngine{
		Name_:  "claude",
		Script: []engine.InvokeResponse{{Text: "coded", UsageTokens: 10}},
	}
	reviewer := approvedReviewer()

	task := &spec.Task{ID: "wd", Kind: "feature", Status: "pending",
		Acceptance: []string{"c1"}}

	const wt = "/tmp/aios-worktree/wd"
	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier:  &stubVerifier{results: []verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		Workdir:   wt,
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}
	if _, err := Run(context.Background(), task, dep); err != nil {
		t.Fatal(err)
	}
	if len(coder.Received) != 1 || coder.Received[0].Workdir != wt {
		t.Errorf("coder Workdir = %q, want %q", coder.Received[0].Workdir, wt)
	}
	if len(reviewer.Received) != 1 || reviewer.Received[0].Workdir != wt {
		t.Errorf("reviewer Workdir = %q, want %q", reviewer.Received[0].Workdir, wt)
	}
}

// TestRun_VerifyRedFeedsSyntheticIssues verifies that when verify is red,
// the failing check shows up in the coder's next-round prompt as a blocking
// issue even if the reviewer approved. Without this, a reviewer that
// approves red code leaves the coder with zero feedback on round N+1.
func TestRun_VerifyRedFeedsSyntheticIssues(t *testing.T) {
	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "r1"}, {Text: "r2"}}}
	// Reviewer approves both rounds. The only reason round 2 happens is that
	// verify is red in round 1 — our new logic must still advance the loop.
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: reviewerApproveJSON}, {Text: reviewerApproveJSON}}}

	verifier := &seqVerifier{results: [][]verify.CheckResult{
		{{Name: "test_cmd", Status: verify.StatusFailed, ExitCode: 1}},
		{{Name: "test_cmd", Status: verify.StatusPassed}},
	}}

	var inputs []CoderInput
	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier:    verifier,
		RenderCoder: func(in CoderInput) string { inputs = append(inputs, in); return "stub" },
		MaxRounds:   5, MaxTokens: 100000, MaxWall: time.Minute,
	}
	task := &spec.Task{ID: "t", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateConverged {
		t.Fatalf("final = %s, want converged (round 2 verify is green)", outcome.Final)
	}
	if len(inputs) != 2 {
		t.Fatalf("coder called %d times, want 2", len(inputs))
	}
	r2 := inputs[1]
	if !r2.IsRevision {
		t.Errorf("round 2 IsRevision = false, want true")
	}
	// Round 2's coder must see the synthetic issue for the failed check,
	// even though the reviewer approved round 1.
	found := false
	for _, i := range r2.Issues {
		if i.Severity == "blocking" && strings.Contains(i.Note, "test_cmd") && strings.Contains(i.Note, "failed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("round 2 coder issues missing synthetic verify issue; got %+v", r2.Issues)
	}
	// Round-1 record should also contain the synthetic issue for audit.
	if len(outcome.Rounds) < 1 {
		t.Fatal("no rounds recorded")
	}
	r1rec := outcome.Rounds[0]
	foundRec := false
	for _, i := range r1rec.Review.Issues {
		if strings.Contains(i.Note, "test_cmd") {
			foundRec = true
			break
		}
	}
	if !foundRec {
		t.Errorf("round 1 audit record missing synthetic verify issue; got %+v", r1rec.Review.Issues)
	}
}

// TestRun_VerifyRedStallsInsteadOfLooping verifies that persistently red
// verify with a reviewer that keeps approving converges to a stall block
// (via the synthetic-issue fingerprint) instead of burning all rounds.
func TestRun_VerifyRedStallsInsteadOfLooping(t *testing.T) {
	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "r1"}, {Text: "r2"}, {Text: "r3"}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: reviewerApproveJSON},
			{Text: reviewerApproveJSON},
			{Text: reviewerApproveJSON},
		}}
	// Same failure every round — fingerprint stable → stall after 3.
	verifier := &stubVerifier{results: []verify.CheckResult{
		{Name: "test_cmd", Status: verify.StatusFailed, ExitCode: 1},
	}}
	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier:  verifier,
		MaxRounds: 10, MaxTokens: 1_000_000, MaxWall: time.Minute,
	}
	task := &spec.Task{ID: "t", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	outcome, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Final != StateBlocked {
		t.Fatalf("final = %s, want blocked", outcome.Final)
	}
	if !strings.HasPrefix(outcome.Reason, "stall_no_progress") {
		t.Errorf("reason = %q, want stall_no_progress prefix", outcome.Reason)
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
