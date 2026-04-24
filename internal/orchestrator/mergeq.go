package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

type MergeRequest struct {
	TaskID    TaskID
	Branch    string
	ParentSHA string
	Diff      []byte
	Ack       chan MergeResult
	// ReReview, if non-nil, is called on rebase paths where the diff changed.
	// It receives the new diff and returns approved=true to allow the merge.
	ReReview func(newDiff []byte) (approved bool, err error)
	// ReVerify, if non-nil, is called after a successful rebase. It runs the
	// project's verify checks against the rebased state and returns passed=true
	// to allow the merge. A rebase can succeed mechanically while still
	// breaking tests; this gate prevents silent correctness loss when two
	// parallel tasks touch overlapping code.
	ReVerify func() (passed bool, summary string)
}

type MergeResult struct {
	Status      string // "converged" | "blocked"
	Reason      string // deprecated: mirror of BlockReason.String()
	BlockReason *BlockReason
}

// blockedMerge builds a blocked MergeResult whose Reason string mirrors the
// new BlockReason.String() — the legacy field is still consumed by code
// that has not been migrated.
func blockedMerge(code BlockCode, detail string) MergeResult {
	br := NewBlock(code, detail)
	return MergeResult{Status: "blocked", Reason: br.String(), BlockReason: br}
}

type MergeQueue struct {
	repoDir       string
	stagingBranch string
	reviewer      engine.Engine // unused for now; kept for v0.1 extension
	in            chan MergeRequest
	closed        chan struct{}
	logSink       func(line string)
}

func NewMergeQueue(repoDir, stagingBranch string, reviewer engine.Engine, logSink func(string)) *MergeQueue {
	if logSink == nil {
		logSink = func(string) {}
	}
	return &MergeQueue{
		repoDir:       repoDir,
		stagingBranch: stagingBranch,
		reviewer:      reviewer,
		in:            make(chan MergeRequest, 64),
		closed:        make(chan struct{}),
		logSink:       logSink,
	}
}

func (q *MergeQueue) Submit(r MergeRequest) {
	q.in <- r
}

func (q *MergeQueue) Close() { close(q.closed) }

func (q *MergeQueue) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.closed:
			return
		case req := <-q.in:
			res := q.process(ctx, req)
			req.Ack <- res
		}
	}
}

func (q *MergeQueue) process(ctx context.Context, req MergeRequest) MergeResult {
	stagingHead, err := q.gitOut(ctx, "rev-parse", q.stagingBranch)
	if err != nil {
		q.logSink(fmt.Sprintf("%s %s", req.TaskID, CodeRevParseFailed))
		return blockedMerge(CodeRevParseFailed, "")
	}
	stagingHead = strings.TrimSpace(stagingHead)
	if stagingHead == req.ParentSHA {
		if err := q.git(ctx, "checkout", "-q", q.stagingBranch); err != nil {
			return blockedMerge(CodeCheckoutStagingFailed, "")
		}
		if err := q.git(ctx, "merge", "--ff-only", req.Branch); err != nil {
			q.logSink(fmt.Sprintf("%s %s", req.TaskID, CodeFFFailed))
			return blockedMerge(CodeFFFailed, "")
		}
		q.logSink(fmt.Sprintf("%s ff-merged", req.TaskID))
		return MergeResult{Status: "converged"}
	}
	// Rebase path: switch to task branch, rebase onto staging.
	if err := q.git(ctx, "checkout", "-q", req.Branch); err != nil {
		return blockedMerge(CodeCheckoutFailed, "")
	}
	if err := q.git(ctx, "rebase", q.stagingBranch); err != nil {
		_ = q.git(ctx, "rebase", "--abort")
		_ = q.git(ctx, "checkout", "-q", q.stagingBranch)
		q.logSink(fmt.Sprintf("%s %s", req.TaskID, CodeRebaseConflict))
		return blockedMerge(CodeRebaseConflict, "")
	}
	newDiff, _ := q.gitOut(ctx, "diff", q.stagingBranch+"..HEAD")
	if normalize(newDiff) != normalize(string(req.Diff)) && req.ReReview != nil {
		approved, err := req.ReReview([]byte(newDiff))
		if err != nil || !approved {
			_ = q.git(ctx, "checkout", "-q", q.stagingBranch)
			q.logSink(fmt.Sprintf("%s %s", req.TaskID, CodeRebaseReviewRejected))
			return blockedMerge(CodeRebaseReviewRejected, "")
		}
	}
	// Re-run verification on the rebased branch HEAD (currently checked out
	// in q.repoDir). Mechanical rebase success does not imply behavioral
	// correctness — overlapping changes can produce a clean merge that still
	// breaks tests.
	if req.ReVerify != nil {
		passed, summary := req.ReVerify()
		if !passed {
			_ = q.git(ctx, "checkout", "-q", q.stagingBranch)
			q.logSink(fmt.Sprintf("%s %s", req.TaskID, CodeRebaseVerifyFailed))
			return blockedMerge(CodeRebaseVerifyFailed, summary)
		}
	}
	if err := q.git(ctx, "checkout", "-q", q.stagingBranch); err != nil {
		return blockedMerge(CodeCheckoutStagingFailed, "")
	}
	if err := q.git(ctx, "merge", "--ff-only", req.Branch); err != nil {
		return blockedMerge(CodeFFAfterRebaseFailed, "")
	}
	q.logSink(fmt.Sprintf("%s rebased-and-merged", req.TaskID))
	return MergeResult{Status: "converged"}
}

func (q *MergeQueue) git(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = q.repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w (%s)", args, err, string(out))
	}
	return nil
}

func (q *MergeQueue) gitOut(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = q.repoDir
	out, err := cmd.Output()
	return string(out), err
}

func normalize(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
