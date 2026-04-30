//go:build windows

package engine

import "os/exec"

// On Windows, process groups work differently (job objects). The default
// CommandContext kill is good enough for now — leave these as no-ops.

func setupProcessGroup(cmd *exec.Cmd)        {}
func killProcessGroup(cmd *exec.Cmd) error   { return nil }
