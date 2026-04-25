package githost

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Compile-time guard: CLIHost satisfies Host. Updated in Tasks 3/4 as
// WaitForChecks and MergePR get real bodies.
var _ Host = (*CLIHost)(nil)

// CLIHost implements Host by shelling out to the `gh` CLI. Callers must have
// `gh` on PATH and a valid authenticated session (`gh auth status` clean).
// Both invariants are enforced by the autopilot preflight, not here.
type CLIHost struct {
	// exec is the command builder. Real usage leaves it nil and falls back to
	// exec.Command. Tests inject a fake to avoid spawning real `gh` processes.
	exec      func(name string, args ...string) *exec.Cmd
	pollEvery time.Duration // 0 = use default (10s)
}

// NewCLIHost returns a CLIHost using the real `os/exec` package.
func NewCLIHost() *CLIHost { return &CLIHost{} }

func (h *CLIHost) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if h.exec != nil {
		return h.exec(name, args...)
	}
	return exec.CommandContext(ctx, name, args...)
}

// ghPRJSON matches the subset of `gh pr create --json number,url` output we use.
type ghPRJSON struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

func (h *CLIHost) OpenPR(ctx context.Context, base, head, title, body string) (*PR, error) {
	cmd := h.cmd(ctx, "gh", "pr", "create",
		"--base", base,
		"--head", head,
		"--title", title,
		"--body", body,
		"--json", "number,url",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w", err)
	}
	var parsed ghPRJSON
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("gh pr create: parse json: %w (raw: %q)", err, string(out))
	}
	return &PR{
		Number: parsed.Number,
		URL:    parsed.URL,
		Head:   head,
		Base:   base,
	}, nil
}

// ghCheckRow matches the subset of `gh pr checks --json bucket` output we use.
// `bucket` is `pass | fail | pending | skipping | cancel`.
type ghCheckRow struct {
	Bucket string `json:"bucket"`
}

// pollInterval returns the configured polling cadence, defaulting to 10s.
// Tests override via the unexported pollEvery field to drive timeout paths
// quickly.
func (h *CLIHost) pollInterval() time.Duration {
	if h.pollEvery > 0 {
		return h.pollEvery
	}
	return 10 * time.Second
}

func (h *CLIHost) WaitForChecks(ctx context.Context, pr *PR, timeout time.Duration) (ChecksState, error) {
	deadline := time.Now().Add(timeout)
	for {
		cmd := h.cmd(ctx, "gh", "pr", "checks", fmt.Sprintf("%d", pr.Number), "--json", "bucket")
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("gh pr checks: %w", err)
		}
		var rows []ghCheckRow
		if err := json.Unmarshal(out, &rows); err != nil {
			return "", fmt.Errorf("gh pr checks: parse json: %w (raw: %q)", err, string(out))
		}
		state := aggregateChecks(rows)
		if state != ChecksPending {
			return state, nil
		}
		if time.Now().After(deadline) {
			return "", ErrChecksTimeout
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(h.pollInterval()):
		}
	}
}

// aggregateChecks applies the precedence rule: any fail/cancel → red; all
// pass/skipping with ≥1 row → green; otherwise pending (keep polling). Empty
// input is pending — gh returns [] for the brief window between PR open and
// the first check kicking off.
func aggregateChecks(rows []ghCheckRow) ChecksState {
	if len(rows) == 0 {
		return ChecksPending
	}
	allPass := true
	for _, r := range rows {
		switch r.Bucket {
		case "fail", "cancel":
			return ChecksRed
		case "pass", "skipping":
			// counted as green-equivalent
		default:
			allPass = false
		}
	}
	if allPass {
		return ChecksGreen
	}
	return ChecksPending
}

// MergePR is implemented in subsequent tasks.
func (h *CLIHost) MergePR(ctx context.Context, pr *PR, mode MergeMode) error {
	return fmt.Errorf("MergePR: not implemented")
}
