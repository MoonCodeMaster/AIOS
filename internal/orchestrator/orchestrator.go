package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Solaxis/aios/internal/engine"
	"github.com/Solaxis/aios/internal/spec"
	"github.com/Solaxis/aios/internal/verify"
)

// stallThreshold is the number of consecutive review rounds with an identical
// issue fingerprint that triggers a "no progress" block. Three rounds is the
// smallest window that reliably distinguishes "the model needs another pass"
// from "the coder cannot resolve what the reviewer is asking for".
const stallThreshold = 3

type Deps struct {
	Coder    engine.Engine
	Reviewer engine.Engine
	Verifier interface {
		Run() []verify.CheckResult
	}
	// Prompt renderers are injected so tests don't need templates on disk.
	RenderCoder    func(in CoderInput) string
	RenderReviewer func(task *spec.Task, diff string, checks []verify.CheckResult) string
	// Diff is a callback returning the current diff for this task's worktree.
	Diff func() (string, error)

	// Workdir is the per-task worktree path. When non-empty it is passed to
	// every engine invocation so the child CLI runs scoped to that directory
	// instead of inheriting the AIOS process cwd.
	Workdir string

	MaxRounds int
	MaxTokens int
	MaxWall   time.Duration
}

// CoderInput is the full per-round context handed to the RenderCoder callback.
// Fresh tasks get IsRevision=false and zero values for the Prev* fields;
// revision rounds get the prior round's diff, verify results, and reviewer
// issues so the prompt can show the coder its previous attempt and what the
// reviewer pushed back on.
type CoderInput struct {
	Task       *spec.Task
	IsRevision bool
	Round      int                  // 1-indexed
	Issues     []ReviewIssue        // reviewer issues from the prior round
	PrevDiff   string               // diff the reviewer saw in the prior round
	PrevChecks []verify.CheckResult // verify results from the prior round
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
	Severity string `json:"severity"`           // "blocking" | "nit"
	Category string `json:"category,omitempty"` // correctness | acceptance | regression | test-coverage | style | security | performance
	Note     string `json:"note"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
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
	var prevFP string
	stallCount := 0
	var prevDiff string
	var prevChecks []verify.CheckResult

	for !TerminalStates[out.Final] {
		if reason := b.ExceededReason(); reason != "" {
			out.Final = StateBlocked
			out.Reason = reason
			break
		}
		b.BumpRound()
		r := RoundRecord{N: b.Rounds()}

		// --- coding ---
		cp := d.RenderCoder(CoderInput{
			Task:       task,
			IsRevision: isRevision,
			Round:      b.Rounds(),
			Issues:     issues,
			PrevDiff:   prevDiff,
			PrevChecks: prevChecks,
		})
		cres, err := d.Coder.Invoke(ctx, engine.InvokeRequest{
			Role:    engine.RoleCoder,
			Prompt:  cp,
			Workdir: d.Workdir,
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
			Role:    engine.RoleReviewer,
			Prompt:  rp,
			Workdir: d.Workdir,
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

		// --- stall detection ---
		// If three consecutive review rounds raise an identical set of unmet
		// criteria + issues, the coder is not making the reviewer happy and
		// further rounds will only burn tokens. Block with a structured reason.
		fp := issueFingerprint(rev)
		if fp != "" && fp == prevFP {
			stallCount++
		} else {
			stallCount = 1
		}
		prevFP = fp
		if stallCount >= stallThreshold {
			out.Final = StateBlocked
			out.Reason = fmt.Sprintf("stall_no_progress: %d consecutive rounds raised identical review issues",
				stallCount)
			break
		}

		issues = rev.Issues
		isRevision = true
		prevDiff = diff
		prevChecks = r.Checks
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

// issueFingerprint reduces a ReviewResult to a stable string signature of
// what the reviewer is unhappy about: unmet criteria IDs plus issue
// (severity, note) pairs, sorted so order changes do not perturb it.
// Returns "" when the reviewer raised nothing — that case is handled by
// the convergence check, not the stall detector.
func issueFingerprint(rev ReviewResult) string {
	parts := make([]string, 0, len(rev.Criteria)+len(rev.Issues))
	for _, c := range rev.Criteria {
		if c.Status != "satisfied" {
			parts = append(parts, "C:"+c.ID)
		}
	}
	for _, i := range rev.Issues {
		parts = append(parts, "I:"+i.Severity+":"+i.Note)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func defaultCoderRender(in CoderInput) string {
	if !in.IsRevision {
		return fmt.Sprintf("Implement task %s. Acceptance: %v\n%s",
			in.Task.ID, in.Task.Acceptance, in.Task.Body)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Revise task %s (round %d).\n", in.Task.ID, in.Round)
	fmt.Fprintf(&b, "Acceptance: %v\n", in.Task.Acceptance)
	if len(in.PrevChecks) > 0 {
		fmt.Fprintf(&b, "Prior verify results:\n")
		for _, c := range in.PrevChecks {
			fmt.Fprintf(&b, "  - %s: %s (exit %d)\n", c.Name, c.Status, c.ExitCode)
		}
	}
	if len(in.Issues) > 0 {
		fmt.Fprintf(&b, "Reviewer issues:\n")
		for _, i := range in.Issues {
			fmt.Fprintf(&b, "  - [%s] %s\n", i.Severity, i.Note)
		}
	}
	if in.PrevDiff != "" {
		fmt.Fprintf(&b, "Your prior attempt (diff):\n%s\n", in.PrevDiff)
	}
	return b.String()
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
