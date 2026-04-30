//go:build darwin && macos_smoke

package engine

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRealEnginesParallelSmoke(t *testing.T) {
	claudeBin := smokeBinary(t, "AIOS_CLAUDE_BIN", "claude")
	codexBin := smokeBinary(t, "AIOS_CODEX_BIN", "codex")
	t.Logf("claude version: %s", smokeVersion(t, claudeBin))
	t.Logf("codex version: %s", smokeVersion(t, codexBin))

	workdir := t.TempDir()
	prompt := "Reply with exactly AIOS_PARALLEL_SMOKE_OK and nothing else."
	req := InvokeRequest{
		Role:    RoleCoder,
		Prompt:  prompt,
		Workdir: workdir,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ra, rb := InvokeParallel(ctx,
		&ClaudeEngine{Binary: claudeBin, TimeoutSec: 150},
		&CodexEngine{Binary: codexBin, TimeoutSec: 150},
		req,
		req,
	)

	assertSmokeResult(t, ra)
	assertSmokeResult(t, rb)
}

func smokeBinary(t *testing.T, envName, fallback string) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		if _, err := exec.LookPath(v); err != nil {
			t.Fatalf("%s=%q is not executable on PATH: %v", envName, v, err)
		}
		return v
	}
	p, err := exec.LookPath(fallback)
	if err != nil {
		t.Fatalf("%s binary not found: %v", fallback, err)
	}
	return p
}

func smokeVersion(t *testing.T, binary string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s --version: %v\n%s", binary, err, out)
	}
	return strings.TrimSpace(string(out))
}

func assertSmokeResult(t *testing.T, r ParallelResult) {
	t.Helper()
	if r.Err != nil {
		t.Fatalf("%s Invoke error after %dms: %v", r.Engine, r.DurationMs, r.Err)
	}
	if r.Response == nil {
		t.Fatalf("%s returned nil response", r.Engine)
	}
	if strings.TrimSpace(r.Response.Text) == "" {
		t.Fatalf("%s returned empty text; raw=%q", r.Engine, truncateBytes([]byte(r.Response.Raw), 400))
	}
	if !strings.Contains(r.Response.Text, "AIOS_PARALLEL_SMOKE_OK") {
		t.Fatalf("%s text = %q, want AIOS_PARALLEL_SMOKE_OK", r.Engine, r.Response.Text)
	}
}
