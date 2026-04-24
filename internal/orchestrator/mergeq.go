package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Solaxis/aios/internal/engine"
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
}

type MergeResult struct {
	Status string // "converged" | "blocked"
	Reason string
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
		q.logSink(fmt.Sprintf("%s rev-parse-failed", req.TaskID))
		return MergeResult{Status: "blocked", Reason: "rev-parse-failed"}
	}
	stagingHead = strings.TrimSpace(stagingHead)
	if stagingHead == req.ParentSHA {
		if err := q.git(ctx, "checkout", "-q", q.stagingBranch); err != nil {
			return MergeResult{Status: "blocked", Reason: "checkout-staging-failed"}
		}
		if err := q.git(ctx, "merge", "--ff-only", req.Branch); err != nil {
			q.logSink(fmt.Sprintf("%s ff-failed", req.TaskID))
			return MergeResult{Status: "blocked", Reason: "ff-failed"}
		}
		q.logSink(fmt.Sprintf("%s ff-merged", req.TaskID))
		return MergeResult{Status: "converged"}
	}
	// Rebase path: switch to task branch, rebase onto staging.
	if err := q.git(ctx, "checkout", "-q", req.Branch); err != nil {
		return MergeResult{Status: "blocked", Reason: "checkout-failed"}
	}
	if err := q.git(ctx, "rebase", q.stagingBranch); err != nil {
		_ = q.git(ctx, "rebase", "--abort")
		_ = q.git(ctx, "checkout", "-q", q.stagingBranch)
		q.logSink(fmt.Sprintf("%s rebase-conflict", req.TaskID))
		return MergeResult{Status: "blocked", Reason: "rebase-conflict"}
	}
	newDiff, _ := q.gitOut(ctx, "diff", q.stagingBranch+"..HEAD")
	if normalize(newDiff) != normalize(string(req.Diff)) && req.ReReview != nil {
		approved, err := req.ReReview([]byte(newDiff))
		if err != nil || !approved {
			_ = q.git(ctx, "checkout", "-q", q.stagingBranch)
			q.logSink(fmt.Sprintf("%s rebase-review-rejected", req.TaskID))
			return MergeResult{Status: "blocked", Reason: "rebase-review-rejected"}
		}
	}
	if err := q.git(ctx, "checkout", "-q", q.stagingBranch); err != nil {
		return MergeResult{Status: "blocked", Reason: "checkout-staging-failed"}
	}
	if err := q.git(ctx, "merge", "--ff-only", req.Branch); err != nil {
		return MergeResult{Status: "blocked", Reason: "ff-after-rebase-failed"}
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
