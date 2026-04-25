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
	exec func(name string, args ...string) *exec.Cmd
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

// WaitForChecks and MergePR are implemented in subsequent tasks.
func (h *CLIHost) WaitForChecks(ctx context.Context, pr *PR, timeout time.Duration) (ChecksState, error) {
	return "", fmt.Errorf("WaitForChecks: not implemented")
}
func (h *CLIHost) MergePR(ctx context.Context, pr *PR, mode MergeMode) error {
	return fmt.Errorf("MergePR: not implemented")
}
