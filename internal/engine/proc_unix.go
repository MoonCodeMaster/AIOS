//go:build !windows

package engine

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures cmd so the spawned engine becomes its own
// process group leader. Without this, descendants of the engine CLI (e.g.
// MCP servers it launches) survive a cancel/timeout because SIGKILL is
// delivered only to the leader.
func setupProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the entire process group of cmd. Used as
// the cmd.Cancel callback so context cancellation reaps grandchildren too.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative pid targets the process group.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
