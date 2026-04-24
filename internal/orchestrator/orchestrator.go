package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/spec"
	"github.com/Solaxis/aios/internal/verify"
)

type Deps struct {
	Coder    engine.Engine
	Reviewer engine.Engine
	Verifier interface {
		Run() []verify.CheckResult
	}
	// Prompt renderers are injected so tests don't need templates on disk.
	RenderCoder   func(task *spec.Task, issues []ReviewIssue, isRevision bool) string
	RenderReviewer func(task *spec.Task, diff string, checks []verify.CheckResult) string
	// Diff is a callback returning the current diff for this task's worktree.
	Diff func() (string, error)

	MaxRounds int
	MaxTokens int
	MaxWall   time.Duration
}

type ReviewResult struct {
	Approved bool              `json:"approved"`
	Criteria []CriterionStatus `json:"criteria"`
	Issues   []ReviewIssue     `json:"issues"`
}

type CriterionStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"` // satisfied | unmet
	Reason string `json:"reason,omitempty"`
}

type ReviewIssue struct {
	Severity string `json:"severity"`
	Note     string `json:"note"`
}

type RoundRecord struct {
	N          int
	CoderText  string
	Review     ReviewResult
	Checks     []verify.CheckResult
	UsageTokens int
}

type Outcome struct {
	Final       State
	Reason      string
	Rounds      []RoundRecord
	UsageTokens int
}

// Run executes the per-task state machine. Dep callbacks are used so that
// worktree + template rendering can be stubbed in tests.
func Run(ctx context.Context, task *spec.Task, d *Deps) (*Outcome, error) {
	if d.RenderCoder == nil {
		d.RenderCoder = defaultCoderRender
	}
	if d.RenderReviewer == nil {
		d.RenderReviewer = defaultReviewerRender
	}
	if d.Diff == nil {
		d.Diff = func() (string, error) { return "", nil }
	}
	b := NewBudget(d.MaxRounds, d.MaxTokens, d.MaxWall)
	out := &Outcome{Final: StatePlanning}
	var issues []ReviewIssue
	isRevision := false

	for !TerminalStates[out.Final] {
		if reason := b.ExceededReason(); reason != "" {
			out.Final = StateBlocked
			out.Reason = reason
			break
		}
		b.BumpRound()
		r := RoundRecord{N: b.Rounds()}

		// --- coding ---
		cp := d.RenderCoder(task, issues, isRevision)
		cres, err := d.Coder.Invoke(ctx, engine.InvokeRequest{
			Role: engine.RoleCoder, Prompt: cp,
		})
		if err != nil {
			return nil, fmt.Errorf("coder invoke: %w", err)
		}
		r.CoderText = cres.Text
		b.AddTokens(cres.UsageTokens)
		r.UsageTokens += cres.UsageTokens

		if reason := b.ExceededReason(); reason != "" {
			out.Rounds = append(out.Rounds, r)
			out.UsageTokens = b.Tokens()
			out.Final = StateBlocked
			out.Reason = reason
			break
		}

		// --- verifying ---
		r.Checks = d.Verifier.Run()

		// --- reviewing ---
		diff, _ := d.Diff()
		rp := d.RenderReviewer(task, diff, r.Checks)
		rres, err := d.Reviewer.Invoke(ctx, engine.InvokeRequest{
			Role: engine.RoleReviewer, Prompt: rp,
		})
		if err != nil {
			return nil, fmt.Errorf("reviewer invoke: %w", err)
		}
		b.AddTokens(rres.UsageTokens)
		r.UsageTokens += rres.UsageTokens
		var rev ReviewResult
		if err := json.Unmarshal([]byte(rres.Text), &rev); err != nil {
			return nil, fmt.Errorf("reviewer JSON parse: %w", err)
		}
		r.Review = rev
		out.Rounds = append(out.Rounds, r)
		out.UsageTokens = b.Tokens()

		// --- decide ---
		if rev.Approved && allSatisfied(rev.Criteria) && verify.AllGreen(r.Checks) {
			out.Final = StateConverged
			break
		}
		issues = rev.Issues
		isRevision = true
	}

	return out, nil
}

func allSatisfied(cs []CriterionStatus) bool {
	for _, c := range cs {
		if c.Status != "satisfied" {
			return false
		}
	}
	return true
}

func defaultCoderRender(task *spec.Task, issues []ReviewIssue, isRevision bool) string {
	if !isRevision {
		return fmt.Sprintf("Implement task %s. Acceptance: %v\n%s", task.ID, task.Acceptance, task.Body)
	}
	return fmt.Sprintf("Revise task %s. Reviewer issues: %v", task.ID, issues)
}

func defaultReviewerRender(task *spec.Task, diff string, checks []verify.CheckResult) string {
	return fmt.Sprintf("Review task %s. Diff:\n%s\nChecks: %+v\nAcceptance: %v",
		task.ID, diff, checks, task.Acceptance)
}

var _ = errors.New // keep import stable across revisions

// TaskFn is the per-task callback that may optionally return a MergeRequest.
// If the returned *MergeRequest is non-nil, RunAll will submit it to the
// MergeQueue, wait for the ack, and rewrite the TaskResult based on the
// merge outcome. If nil, the TaskResult is used as-is.
type TaskFn func(ctx context.Context, id TaskID) (TaskResult, *MergeRequest)

// RunAllOpts controls a multi-task run.
type RunAllOpts struct {
	RepoDir       string
	StagingBranch string
	Tasks         []*spec.Task
	Workers       int
	Reviewer      engine.Engine
	// Task is the preferred per-task callback. If set, it is used and Work is
	// ignored. The callback may return a non-nil *MergeRequest; RunAll will
	// then submit it to the MergeQueue and rewrite the result based on the ack.
	Task TaskFn
	// Work is a backward-compatible shim. If Task is nil and Work is set,
	// RunAll wraps Work as a TaskFn that always returns a nil *MergeRequest.
	// Real production callers wire this to a closure that drives the per-task
	// state machine.
	Work WorkFunc
}

type RunReport struct {
	Converged []TaskID
	Blocked   map[TaskID]string // id -> reason
}

func RunAll(ctx context.Context, opts RunAllOpts) (*RunReport, error) {
	sched, err := NewScheduler(opts.Tasks)
	if err != nil {
		return nil, err
	}
	mq := NewMergeQueue(opts.RepoDir, opts.StagingBranch, opts.Reviewer, nil)
	mqCtx, mqCancel := context.WithCancel(ctx)
	defer mqCancel()
	go mq.Run(mqCtx)

	// Resolve which TaskFn to use. If Task is set, use it directly. Otherwise
	// wrap Work as a TaskFn that always returns a nil *MergeRequest.
	taskFn := opts.Task
	if taskFn == nil && opts.Work != nil {
		work := opts.Work
		taskFn = func(ctx context.Context, id TaskID) (TaskResult, *MergeRequest) {
			return work(ctx, id), nil
		}
	}

	// workFn is the closure passed to the Pool. It calls the TaskFn and, if the
	// TaskFn returns a non-nil MergeRequest, submits it to the MergeQueue and
	// rewrites the TaskResult based on the ack.
	workFn := func(ctx context.Context, id TaskID) TaskResult {
		res, mreq := taskFn(ctx, id)
		if mreq == nil {
			return res
		}
		ack := make(chan MergeResult, 1)
		mreq.Ack = ack
		mq.Submit(*mreq)
		mres := <-ack
		if mres.Status == "blocked" {
			return TaskResult{ID: id, Status: "blocked", Reason: mres.Reason}
		}
		// Merge succeeded — preserve the original status (typically "converged").
		return res
	}

	pool := NewPool(opts.Workers, sched, workFn)
	if err := pool.Run(ctx); err != nil && err != context.Canceled {
		return nil, err
	}

	rep := &RunReport{Blocked: map[TaskID]string{}}
	blocked := sched.Blocked()
	for _, t := range opts.Tasks {
		if _, b := blocked[t.ID]; b {
			rep.Blocked[t.ID] = "blocked"
			continue
		}
		rep.Converged = append(rep.Converged, t.ID)
	}
	return rep, nil
}
