package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
)

// defaultStallThreshold is the number of consecutive review rounds with an
// identical issue fingerprint that triggers a "no progress" block. Three
// rounds is the smallest window that reliably distinguishes "the model needs
// another pass" from "the coder cannot resolve what the reviewer is asking
// for". Overridable per-run via Deps.StallThreshold.
const defaultStallThreshold = 3

type Deps struct {
	Coder    engine.Engine
	Reviewer engine.Engine
	Verifier interface {
		Run() []verify.CheckResult
	}
	// Prompt renderers are injected so tests don't need templates on disk.
	RenderCoder    func(in CoderInput) string
	RenderReviewer func(task *spec.Task, diff string, checks []verify.CheckResult, mcpFailures []engine.McpCall) string
	// Diff is a callback returning the current diff for this task's worktree.
	Diff func() (string, error)

	// Workdir is the per-task worktree path. When non-empty it is passed to
	// every engine invocation so the child CLI runs scoped to that directory
	// instead of inheriting the AIOS process cwd.
	Workdir string

	MaxRounds int
	MaxTokens int
	MaxWall   time.Duration

	// StallThreshold overrides the package default (3) for "consecutive
	// identical-fingerprint rounds that trigger stall detection". Zero = use
	// the package default. Lower values fail faster on hopeless tasks.
	StallThreshold int
	// MaxEscalations is the number of hard-constraint retry rounds to run
	// after stall detection fires before blocking. Zero disables escalation
	// entirely (original pre-P0 behavior). A typical config value is 1.
	MaxEscalations int
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
	// Escalated is true when this round is an escalation retry triggered by
	// stall detection. Prompts rendered for escalated rounds surface the
	// reviewer's outstanding issues as hard constraints that MUST each be
	// addressed; the model is explicitly told that further repetition of
	// the same pattern will block the task.
	Escalated bool
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
	N           int
	CoderPrompt string // prompt string sent to the coder engine this round
	CoderText   string // assistant text extracted from the coder response
	CoderRaw    string // full raw stdout from the coder CLI (audit trail)
	// Escalated is true when this round was an escalation retry triggered by
	// stall detection. The coder prompt for escalated rounds surfaces the
	// reviewer's outstanding issues as hard constraints.
	Escalated      bool
	ReviewerPrompt string       // prompt string sent to the reviewer engine this round
	ReviewerRaw    string       // full raw stdout from the reviewer CLI
	Review         ReviewResult // parsed review verdict
	Checks         []verify.CheckResult
	UsageTokens    int
	// McpCalls captures every MCP tool call the coder made this round
	// (success and failure alike). Failed calls — Error != "" or Denied — are
	// surfaced into the reviewer prompt so the reviewer can distinguish a
	// coder mistake from an MCP outage.
	McpCalls []engine.McpCall
	// CoderAttempts records failed invocation attempts when the engine retry
	// layer had to retry before succeeding. Empty on first-try success.
	CoderAttempts []engine.Attempt
	// ReviewerAttempts is the reviewer-side equivalent of CoderAttempts.
	ReviewerAttempts []engine.Attempt
}

type Outcome struct {
	Final       State
	Reason      string       // deprecated: mirror of BlockReason.String() when blocked
	BlockReason *BlockReason // nil when converged; structured reason when blocked
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
	stallThreshold := d.StallThreshold
	if stallThreshold <= 0 {
		stallThreshold = defaultStallThreshold
	}
	escalationsRemaining := d.MaxEscalations
	out := &Outcome{Final: StatePlanning}
	var issues []ReviewIssue
	isRevision := false
	var prevFP string
	stallCount := 0
	var prevDiff string
	var prevChecks []verify.CheckResult
	// nextEscalated carries over the escalation flag from the stall-detection
	// branch below into the next loop iteration's CoderInput, so the prompt
	// renderer can surface reviewer issues as hard constraints for that one
	// round. Reset automatically after use.
	nextEscalated := false

	for !TerminalStates[out.Final] {
		if br := budgetBlock(b); br != nil {
			out.Final = StateBlocked
			out.BlockReason = br
			out.Reason = br.String()
			break
		}
		b.BumpRound()
		r := RoundRecord{N: b.Rounds(), Escalated: nextEscalated}

		// --- coding ---
		cp := d.RenderCoder(CoderInput{
			Task:       task,
			IsRevision: isRevision,
			Round:      b.Rounds(),
			Issues:     issues,
			PrevDiff:   prevDiff,
			PrevChecks: prevChecks,
			Escalated:  nextEscalated,
		})
		// Consumed this round — do not carry the flag into the next iteration
		// unless the stall-detection branch below fires again.
		nextEscalated = false
		r.CoderPrompt = cp
		cres, err := d.Coder.Invoke(ctx, engine.InvokeRequest{
			Role:    engine.RoleCoder,
			Prompt:  cp,
			Workdir: d.Workdir,
		})
		if err != nil {
			return nil, fmt.Errorf("coder invoke: %w", err)
		}
		r.CoderText = cres.Text
		r.CoderRaw = cres.Raw
		r.CoderAttempts = cres.Attempts
		b.AddTokens(cres.UsageTokens)
		r.UsageTokens += cres.UsageTokens

		if br := budgetBlock(b); br != nil {
			out.Rounds = append(out.Rounds, r)
			out.UsageTokens = b.Tokens()
			out.Final = StateBlocked
			out.BlockReason = br
			out.Reason = br.String()
			break
		}

		// --- verifying ---
		r.Checks = d.Verifier.Run()

		// Capture MCP calls from the coder's response so the audit trail and
		// the reviewer prompt both see them.
		r.McpCalls = cres.McpCalls
		var mcpFailures []engine.McpCall
		for _, m := range cres.McpCalls {
			if m.Error != "" || m.Denied {
				mcpFailures = append(mcpFailures, m)
			}
		}
		// --- reviewing ---
		diff, _ := d.Diff()
		rp := d.RenderReviewer(task, diff, r.Checks, mcpFailures)
		r.ReviewerPrompt = rp
		rres, err := d.Reviewer.Invoke(ctx, engine.InvokeRequest{
			Role:    engine.RoleReviewer,
			Prompt:  rp,
			Workdir: d.Workdir,
		})
		if err != nil {
			return nil, fmt.Errorf("reviewer invoke: %w", err)
		}
		r.ReviewerRaw = rres.Raw
		r.ReviewerAttempts = rres.Attempts
		b.AddTokens(rres.UsageTokens)
		r.UsageTokens += rres.UsageTokens
		var rev ReviewResult
		if err := json.Unmarshal([]byte(rres.Text), &rev); err != nil {
			return nil, fmt.Errorf("reviewer JSON parse: %w", err)
		}

		// Fold verify failures into reviewer issues. When verify is red, the
		// reviewer may miss it or (worse) approve anyway; synthesising blocking
		// issues from the failed checks guarantees the next round's coder
		// prompt shows the failures verbatim, and that the stall fingerprint
		// reflects them so an endlessly-red verify converges to a structured
		// block instead of looping on empty reviewer issues.
		if !verify.AllGreen(r.Checks) {
			synth := verifyFailureIssues(r.Checks)
			rev.Issues = append(synth, rev.Issues...)
		}
		r.Review = rev
		out.Rounds = append(out.Rounds, r)
		out.UsageTokens = b.Tokens()

		// --- decide ---
		if rev.Approved && allSatisfied(rev.Criteria) && verify.AllGreen(r.Checks) {
			out.Final = StateConverged
			break
		}

		// --- stall detection & escalation ladder ---
		// If N consecutive review rounds raise an identical set of unmet
		// criteria + issues, the coder is not making the reviewer happy and
		// further normal rounds will only burn tokens. Before blocking, try
		// up to MaxEscalations "hard-constraint retries" — rounds whose coder
		// prompt surfaces the reviewer's outstanding issues as hard blockers
		// that MUST each be addressed. If an escalation succeeds, the loop
		// converges normally. If escalations are exhausted or disabled, block
		// with a structured reason whose Detail carries a human-readable
		// summary of the unresolved issues — so `aios status` / report
		// renderers can show "why this needs a human" without further I/O.
		fp := issueFingerprint(rev)
		if fp != "" && fp == prevFP {
			stallCount++
		} else {
			stallCount = 1
		}
		prevFP = fp
		if stallCount >= stallThreshold {
			if escalationsRemaining > 0 {
				escalationsRemaining--
				// Reset the counter so the escalated round gets a fresh
				// stall window: if it produces NEW issues we treat that as
				// progress; if it produces the SAME fingerprint we re-enter
				// this branch with escalationsRemaining now 0 and block.
				stallCount = 0
				prevFP = ""
				nextEscalated = true
				// Fall through to the "carry forward" block below — the
				// loop continues with an escalated next round.
			} else {
				br := NewBlock(CodeStallNoProgress, summarizeStall(stallCount, rev, d.MaxEscalations))
				out.Final = StateBlocked
				out.BlockReason = br
				out.Reason = br.String()
				break
			}
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

// verifyFailureIssues returns a ReviewIssue for every check that did not
// pass, so the downstream pipeline (next-round coder prompt + stall
// fingerprint) sees failed verify as blocking feedback even when the
// reviewer did not report it.
func verifyFailureIssues(checks []verify.CheckResult) []ReviewIssue {
	var out []ReviewIssue
	for _, c := range checks {
		switch c.Status {
		case verify.StatusFailed, verify.StatusTimedOut:
			out = append(out, ReviewIssue{
				Severity: "blocking",
				Category: "regression",
				Note:     "verify check " + c.Name + " " + string(c.Status),
			})
		}
	}
	return out
}

// summarizeStall renders the human-readable block detail for a stall. It
// records how many rounds raised the same fingerprint, how many escalation
// retries were tried, and the top unresolved reviewer concerns (unmet
// criteria + blocking issues) so downstream consumers — report renderer,
// CLI summary, future auto-decompose — have enough structured context to
// explain "why this task needs a human" without re-reading round files.
func summarizeStall(stallCount int, rev ReviewResult, maxEscalations int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d consecutive rounds raised identical review issues", stallCount)
	if maxEscalations > 0 {
		fmt.Fprintf(&b, "; %d escalation(s) exhausted", maxEscalations)
	}
	var unmet []string
	for _, c := range rev.Criteria {
		if c.Status != "satisfied" {
			if c.Reason != "" {
				unmet = append(unmet, fmt.Sprintf("%s (%s)", c.ID, c.Reason))
			} else {
				unmet = append(unmet, c.ID)
			}
		}
	}
	if len(unmet) > 0 {
		fmt.Fprintf(&b, "; unmet criteria: %s", strings.Join(unmet, ", "))
	}
	var blockers []string
	for _, i := range rev.Issues {
		if i.Severity == "blocking" {
			note := i.Note
			if i.File != "" {
				note = i.File + ": " + note
			}
			blockers = append(blockers, note)
		}
	}
	if len(blockers) > 0 {
		// Cap at 5 so a reviewer that dumps 30 issues does not blow up the
		// BlockReason detail field (which shows up in CLI output verbatim).
		const max = 5
		shown := blockers
		suffix := ""
		if len(shown) > max {
			shown = shown[:max]
			suffix = fmt.Sprintf(" (+%d more)", len(blockers)-max)
		}
		fmt.Fprintf(&b, "; blocking issues: %s%s", strings.Join(shown, " | "), suffix)
	}
	return b.String()
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

func defaultReviewerRender(task *spec.Task, diff string, checks []verify.CheckResult, mcpFailures []engine.McpCall) string {
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
	// Blocked maps every blocked task ID (direct or transitive) to its
	// structured BlockReason. Tasks that failed directly carry the concrete
	// code (e.g. CodeMaxRoundsExceeded); tasks that were cascaded by the DAG
	// carry CodeUpstreamBlocked with Upstream set to the triggering task.
	Blocked map[TaskID]BlockReason
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
			return TaskResult{
				ID:          id,
				Status:      "blocked",
				Reason:      mres.Reason,
				BlockReason: mres.BlockReason,
			}
		}
		// Merge succeeded — preserve the original status (typically "converged").
		return res
	}

	pool := NewPool(opts.Workers, sched, workFn)
	if err := pool.Run(ctx); err != nil && err != context.Canceled {
		return nil, err
	}

	rep := &RunReport{Blocked: map[TaskID]BlockReason{}}
	for id, reason := range sched.Blocked() {
		rep.Blocked[id] = reason
	}
	for _, t := range opts.Tasks {
		if _, b := rep.Blocked[t.ID]; b {
			continue
		}
		rep.Converged = append(rep.Converged, t.ID)
	}
	return rep, nil
}
