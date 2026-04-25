package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// autopilotPreflight enforces the runtime invariants the autopilot finalizer
// depends on, before any model invocation. Indirections are exposed as fields
// so the unit test can inject fakes for `gh`, `git remote`, etc.
type autopilotPreflight struct {
	lookPath  func(string) (string, error)
	runCmd    func(*exec.Cmd) error
	hasRemote func() (bool, error)
}

func newAutopilotPreflight(repoDir string) *autopilotPreflight {
	return &autopilotPreflight{
		lookPath: exec.LookPath,
		runCmd:   func(c *exec.Cmd) error { return c.Run() },
		hasRemote: func() (bool, error) {
			out, err := exec.Command("git", "-C", repoDir, "remote").Output()
			if err != nil {
				return false, err
			}
			return len(strings.TrimSpace(string(out))) > 0, nil
		},
	}
}

func (p *autopilotPreflight) Check() error {
	if _, err := p.lookPath("gh"); err != nil {
		return fmt.Errorf("autopilot mode requires the 'gh' CLI on PATH (install: https://cli.github.com): %w", err)
	}
	cmd := exec.CommandContext(context.Background(), "gh", "auth", "status")
	if err := p.runCmd(cmd); err != nil {
		return fmt.Errorf("autopilot mode requires an authenticated 'gh' session — run `gh auth login`: %w", err)
	}
	hasRemote, err := p.hasRemote()
	if err != nil {
		return fmt.Errorf("checking git remote: %w", err)
	}
	if !hasRemote {
		return fmt.Errorf("autopilot mode requires a git remote on the current repository (configure one with `git remote add origin …`)")
	}
	return nil
}
