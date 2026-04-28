package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
