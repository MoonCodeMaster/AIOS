package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hangingBinary writes a shell script that ignores its args and sleeps 30s,
// simulating a wedged engine CLI. The TimeoutSec test relies on this to
// prove a deadline interrupts the child process.
func hangingBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script not portable to windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hang.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write hang script: %v", err)
	}
	return path
}

// TestClaudeInvoke_TimeoutSecFires proves that a hung child process is
// killed when TimeoutSec elapses, instead of blocking the caller forever.
// This was the root cause of the AIOS REPL "stuck on draft-claude" bug:
// TimeoutSec was configured but never wired into a deadline.
func TestClaudeInvoke_TimeoutSecFires(t *testing.T) {
	eng := &ClaudeEngine{
		Binary:     hangingBinary(t),
		TimeoutSec: 1,
	}
	start := time.Now()
	_, err := eng.Invoke(context.Background(), InvokeRequest{Prompt: "x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected friendly timeout message, got: %v", err)
	}
	// Should fire well before the 30s sleep would naturally complete.
	if elapsed > 10*time.Second {
		t.Errorf("Invoke took %s, expected ~1s (timeout did not fire)", elapsed)
	}
}

func TestCodexInvoke_TimeoutSecFires(t *testing.T) {
	eng := &CodexEngine{
		Binary:     hangingBinary(t),
		TimeoutSec: 1,
	}
	start := time.Now()
	_, err := eng.Invoke(context.Background(), InvokeRequest{Prompt: "x"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected friendly timeout message, got: %v", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Invoke took %s, expected ~1s", elapsed)
	}
}

// spawningBinary writes a script that backgrounds a long sleep, records the
// child PID, then sleeps itself. Used to verify that a timeout reaps the
// engine's descendants, not just the leader.
func spawningBinary(t *testing.T, pidFile string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script not portable to windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "spawn.sh")
	script := fmt.Sprintf("#!/bin/sh\nsleep 30 &\necho $! > %s\nwait\n", pidFile)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write spawn script: %v", err)
	}
	return path
}

// pidAlive returns true if pid is still running.
func pidAlive(pid int) bool {
	// signal 0 is the canonical "is this pid alive" probe.
	return syscall.Kill(pid, 0) == nil
}

// TestClaudeInvoke_KillsDescendants proves that a timeout reaps grandchildren
// the engine CLI spawned, not just the leader. Without process-group setup,
// a backgrounded child of the engine survives the cancel and runs to its
// natural completion (here, 30s).
func TestClaudeInvoke_KillsDescendants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group reaping is unix-only")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	eng := &ClaudeEngine{
		Binary:     spawningBinary(t, pidFile),
		TimeoutSec: 1,
	}
	_, _ = eng.Invoke(context.Background(), InvokeRequest{Prompt: "x"})

	// Give the kernel a moment to deliver SIGKILL and reap the descendant.
	time.Sleep(300 * time.Millisecond)

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}
	if pidAlive(pid) {
		// Best-effort cleanup so the test doesn't leak a 30s sleep.
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("descendant pid %d still alive after timeout — process group not reaped", pid)
	}
}
