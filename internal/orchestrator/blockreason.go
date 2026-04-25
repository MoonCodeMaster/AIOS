package orchestrator

// BlockCode is the stable, machine-friendly category of a block. New codes
// must be added here rather than constructed inline so that downstream
// consumers (report renderer, scheduler cascade, CI/telemetry) can switch on
// a bounded set instead of parsing free-form strings.
type BlockCode string

const (
	// Budget exhaustion.
	CodeMaxRoundsExceeded BlockCode = "max_rounds_exceeded"
	CodeMaxTokensExceeded BlockCode = "max_tokens_exceeded"
	CodeMaxWallExceeded   BlockCode = "max_wall_exceeded"

	// Review/loop control.
	CodeStallNoProgress BlockCode = "stall_no_progress"

	// Autopilot rescue. Distinct from blocks: a task that hit CodeStallNoProgress
	// and was rescued by `--autopilot` returns Status="blocked" with this code so
	// the scheduler cascades dependents (rather than running them on a base that
	// doesn't contain the parent's work), but the CLI's autopilot path treats it
	// as a non-fatal drop rather than a real block.
	CodeAbandonedAutopilot BlockCode = "abandoned_autopilot"

	// Merge queue.
	CodeRebaseConflict        BlockCode = "rebase_conflict"
	CodeRebaseVerifyFailed    BlockCode = "rebase_verify_failed"
	CodeRebaseReviewRejected  BlockCode = "rebase_review_rejected"
	CodeFFFailed              BlockCode = "ff_failed"
	CodeFFAfterRebaseFailed   BlockCode = "ff_after_rebase_failed"
	CodeCheckoutFailed        BlockCode = "checkout_failed"
	CodeCheckoutStagingFailed BlockCode = "checkout_staging_failed"
	CodeRevParseFailed        BlockCode = "rev_parse_failed"

	// Task preflight / setup errors.
	CodeTaskNotFound      BlockCode = "task_not_found"
	CodeWorktreeAddFailed BlockCode = "worktree_add_failed"
	CodeMcpScopeFailed    BlockCode = "mcp_scope_failed"
	CodeEnginePickFailed  BlockCode = "engine_pick_failed"
	CodeGitAddFailed      BlockCode = "git_add_failed"
	CodeCommitFailed      BlockCode = "commit_failed"

	// Engine errors surfacing from orchestrator.Run.
	CodeEngineInvokeFailed BlockCode = "engine_invoke_failed"

	// DAG cascade — this task is blocked because a dependency is.
	CodeUpstreamBlocked BlockCode = "upstream_blocked"
)

// BlockReason is a structured description of why a task or merge was
// blocked. Code is the category; Detail carries free-form context (a verify
// summary, an underlying error message) that callers may surface verbatim;
// Upstream is populated when Code == CodeUpstreamBlocked and names the
// root-cause TaskID that transitively blocked this task.
//
// The legacy `Reason string` fields on TaskResult / MergeResult / Outcome
// are still populated from BlockReason.String() for backward compatibility;
// new code should prefer BlockReason directly.
type BlockReason struct {
	Code     BlockCode `json:"code"`
	Detail   string    `json:"detail,omitempty"`
	Upstream TaskID    `json:"upstream,omitempty"`
}

// String renders a human-readable form: "<code>", "<code>: <detail>",
// "<code>:<upstream>", or "<code>:<upstream>: <detail>".
func (r BlockReason) String() string {
	s := string(r.Code)
	if r.Upstream != "" {
		s = s + ":" + string(r.Upstream)
	}
	if r.Detail != "" {
		s = s + ": " + r.Detail
	}
	return s
}

// NewBlock is a convenience constructor returning a pointer to a BlockReason
// with the given code and detail.
func NewBlock(code BlockCode, detail string) *BlockReason {
	return &BlockReason{Code: code, Detail: detail}
}

// NewUpstreamBlock is the cascade-path constructor. upstream is the TaskID
// of the direct parent whose block caused this task to be blocked.
func NewUpstreamBlock(upstream TaskID) *BlockReason {
	return &BlockReason{Code: CodeUpstreamBlocked, Upstream: upstream}
}
