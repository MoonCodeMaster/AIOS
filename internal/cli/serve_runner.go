package cli

import (
	"context"
	"fmt"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

// AutopilotStatus is the outcome of one autopilot run.
type AutopilotStatus int

const (
	AutopilotUnknown AutopilotStatus = iota
	AutopilotMerged
	AutopilotPRRed
	AutopilotAbandoned
)

// AutopilotResult is the parsed outcome of one autopilot subprocess run.
type AutopilotResult struct {
	Status     AutopilotStatus
	PRNumber   int
	PRURL      string
	AuditTrail string
}

// AutopilotFn runs autopilot for one idea string and returns the parsed result.
type AutopilotFn func(ctx context.Context, idea string) (AutopilotResult, error)

// ServeRunner ties together a host, state, config, and autopilot callback.
type ServeRunner struct {
	Host      githost.Host
	State     *ServeState
	Config    *ServeConfig
	Autopilot AutopilotFn
}

// RunIssue claims an issue, runs autopilot, applies the label state machine
// and final actions (comment, close, open stuck issue), and clears the state
// entry. On autopilot error, the issue is released back to aios:do.
func (r *ServeRunner) RunIssue(ctx context.Context, issue githost.Issue) error {
	labels := r.Config.Labels
	if err := r.Host.RemoveLabel(ctx, issue.Number, labels.Do); err != nil {
		return fmt.Errorf("remove %s: %w", labels.Do, err)
	}
	if err := r.Host.AddLabel(ctx, issue.Number, labels.InProgress); err != nil {
		return fmt.Errorf("add %s: %w", labels.InProgress, err)
	}
	r.State.Add(issue.Number, fmt.Sprintf("issue-%d", issue.Number))

	idea := renderIdea(issue)
	result, err := r.Autopilot(ctx, idea)
	if err != nil {
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.Do)
		r.State.Remove(issue.Number)
		return fmt.Errorf("autopilot: %w", err)
	}

	switch result.Status {
	case AutopilotMerged:
		_ = r.Host.AddComment(ctx, issue.Number, fmt.Sprintf("Merged in #%d (%s); closing.", result.PRNumber, result.PRURL))
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.Done)
		_ = r.Host.CloseIssue(ctx, issue.Number)
	case AutopilotPRRed:
		_ = r.Host.AddComment(ctx, issue.Number, fmt.Sprintf("PR #%d (%s) open; CI failing or timed out — needs human review.", result.PRNumber, result.PRURL))
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.PROpen)
	case AutopilotAbandoned:
		stuckBody := fmt.Sprintf("Original issue: #%d\n\nAutopilot abandoned after exhausted retries and decompose attempts.\n\n%s",
			issue.Number, result.AuditTrail)
		stuckNum, err := r.Host.OpenIssue(ctx, fmt.Sprintf("[aios:stuck] %s", issue.Title), stuckBody, []string{labels.Stuck})
		if err != nil {
			return fmt.Errorf("open stuck issue: %w", err)
		}
		_ = r.Host.AddComment(ctx, issue.Number, fmt.Sprintf("Couldn't converge; full audit trail in #%d.", stuckNum))
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.Stuck)
	default:
		return fmt.Errorf("autopilot returned unknown status %d", result.Status)
	}
	r.State.Remove(issue.Number)
	return nil
}

func renderIdea(issue githost.Issue) string {
	if issue.Body == "" {
		return issue.Title
	}
	return issue.Title + "\n\n" + issue.Body
}
