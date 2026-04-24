package verify

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

type Status string

const (
	StatusPassed        Status = "passed"
	StatusFailed        Status = "failed"
	StatusSkipped       Status = "skipped"         // user marked skipped in config
	StatusNotConfigured Status = "not_configured"  // cmd is empty; treat as pass
	StatusTimedOut      Status = "timed_out"
)

type Check struct {
	Name    string // "test_cmd", "lint_cmd", ...
	Cmd     string // shell cmdline; "" means not configured
	Skipped bool   // user opt-out for this project
}

type CheckResult struct {
	Name     string
	Status   Status
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// Run executes each check in order; always returns one CheckResult per input.
// Each check gets timeout applied individually.
func Run(ctx context.Context, workdir string, checks []Check, timeout time.Duration) []CheckResult {
	out := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		out = append(out, runOne(ctx, workdir, c, timeout))
	}
	return out
}

func runOne(ctx context.Context, workdir string, c Check, timeout time.Duration) CheckResult {
	r := CheckResult{Name: c.Name}
	if c.Skipped {
		r.Status = StatusSkipped
		return r
	}
	if strings.TrimSpace(c.Cmd) == "" {
		r.Status = StatusNotConfigured
		return r
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", c.Cmd)
	cmd.Dir = workdir
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	start := time.Now()
	err := cmd.Run()
	r.Duration = time.Since(start)
	r.Stdout = so.String()
	r.Stderr = se.String()
	if cctx.Err() == context.DeadlineExceeded {
		r.Status = StatusTimedOut
		return r
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			r.ExitCode = ee.ExitCode()
		} else {
			r.ExitCode = -1
		}
		r.Status = StatusFailed
		return r
	}
	r.Status = StatusPassed
	return r
}

// AllGreen returns true iff no check has StatusFailed or StatusTimedOut.
func AllGreen(rs []CheckResult) bool {
	for _, r := range rs {
		switch r.Status {
		case StatusFailed, StatusTimedOut:
			return false
		}
	}
	return true
}
