package worktree

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Git is a thin wrapper around `git`. Every call shells out; errors include
// the exact command and stderr to make failures easy to reproduce.
type Git struct {
	Dir string // working directory (-C); required
}

func (g *Git) Run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.Dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("git %v: %w (stderr: %s)", args, err, stderr.String())
	}
	return stdout.String(), nil
}
