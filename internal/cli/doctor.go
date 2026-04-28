package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/spf13/cobra"
)

// `aios doctor` runs every preflight an interactive user would have to
// debug by hand: are the engines on PATH and answering, is gh authenticated,
// is the repo properly initialised, is the config schema-current. It is
// the cheapest path from "I just installed AIOS" to "AIOS works on this
// machine" — and from a bug report to a triaged bug report.
//
// Exit codes:
//   0 — everything passes (warnings allowed)
//   1 — one or more required checks failed
func newDoctorCmd() *cobra.Command {
	c := &cobra.Command{
		Use:         "doctor",
		Short:       "Diagnose this machine: engines, auth, repo, config",
		Annotations: map[string]string{gateAnnotation: gateLevelGit},
		Long: `aios doctor reports the status of every prerequisite in turn:

  - git ≥ 2.40 (required for worktree behaviour)
  - claude CLI on PATH and answering a tiny no-op call
  - codex  CLI on PATH and answering a tiny no-op call
  - gh CLI on PATH and authenticated (required for autopilot/serve)
  - git remote configured (required for autopilot/serve)
  - .aios/config.toml present and at the current schema version

Each check prints PASS / WARN / FAIL with a one-line reason. Doctor exits
non-zero if any required check failed; warnings (e.g. missing gh, no remote)
do not fail the run because plain ` + "`aios run`" + ` does not need them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := newDoctor(cmd.OutOrStdout())
			if skip, _ := cmd.Flags().GetBool("skip-engine-smoke"); skip {
				d.skipEngineSmoke = true
			}
			if t, _ := cmd.Flags().GetInt("smoke-timeout-sec"); t > 0 {
				d.smokeTimeout = time.Duration(t) * time.Second
			}
			d.runAll(cmd.Context())
			ok := d.report()
			if !ok {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().Bool("skip-engine-smoke", false, "skip the no-op claude/codex calls (faster; only checks binaries are on PATH)")
	c.Flags().Int("smoke-timeout-sec", 30, "timeout for each engine smoke call")
	return c
}

// status of a single doctor check.
type doctorStatus int

const (
	statusPass doctorStatus = iota
	statusWarn
	statusFail
)

func (s doctorStatus) tag() string {
	switch s {
	case statusPass:
		return "PASS"
	case statusWarn:
		return "WARN"
	default:
		return "FAIL"
	}
}

// check is one row in the doctor report. Required checks fail the run when
// they are FAIL; non-required checks downgrade FAIL to WARN.
type check struct {
	Name     string
	Status   doctorStatus
	Detail   string
	Required bool
}

type doctor struct {
	out             io.Writer
	checks          []check
	skipEngineSmoke bool
	smokeTimeout    time.Duration
}

func newDoctor(out io.Writer) *doctor {
	return &doctor{out: out, smokeTimeout: 30 * time.Second}
}

func (d *doctor) add(c check) {
	if !c.Required && c.Status == statusFail {
		c.Status = statusWarn
	}
	d.checks = append(d.checks, c)
}

// runAll runs every check in turn. Cheap, sequential, ordered so the
// earliest failure that explains later failures lands first in the report.
func (d *doctor) runAll(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	d.checkGit()
	d.checkEngineBinary("claude")
	d.checkEngineBinary("codex")
	d.checkGH()
	d.checkGHAuth(ctx)
	d.checkGitRemote()
	cfg := d.checkConfig()
	if !d.skipEngineSmoke {
		d.checkEngineSmoke(ctx, "claude", cfg)
		d.checkEngineSmoke(ctx, "codex", cfg)
	}
}

func (d *doctor) checkGit() {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		d.add(check{Name: "git installed", Status: statusFail, Detail: err.Error(), Required: true})
		return
	}
	v := strings.TrimSpace(string(out))
	d.add(check{Name: "git installed", Status: statusPass, Detail: v, Required: true})
	if maj, min, ok := parseGitVersion(v); ok {
		if maj > 2 || (maj == 2 && min >= 40) {
			d.add(check{Name: "git ≥ 2.40", Status: statusPass, Required: true})
		} else {
			d.add(check{Name: "git ≥ 2.40", Status: statusFail, Detail: v + " (worktree GC depends on 2.40+)", Required: true})
		}
	} else {
		d.add(check{Name: "git ≥ 2.40", Status: statusWarn, Detail: "could not parse version: " + v, Required: false})
	}
}

func (d *doctor) checkEngineBinary(name string) {
	bin, err := exec.LookPath(name)
	if err != nil {
		d.add(check{
			Name:     name + " on PATH",
			Status:   statusFail,
			Detail:   "install: see https://github.com/MoonCodeMaster/AIOS#install",
			Required: true,
		})
		return
	}
	d.add(check{Name: name + " on PATH", Status: statusPass, Detail: bin, Required: true})
}

func (d *doctor) checkGH() {
	bin, err := exec.LookPath("gh")
	if err != nil {
		d.add(check{
			Name:     "gh on PATH",
			Status:   statusWarn,
			Detail:   "needed for `aios autopilot` and `aios serve`; not for plain `aios run`",
			Required: false,
		})
		return
	}
	d.add(check{Name: "gh on PATH", Status: statusPass, Detail: bin, Required: false})
}

func (d *doctor) checkGHAuth(ctx context.Context) {
	if _, err := exec.LookPath("gh"); err != nil {
		return // already reported as a warning above; don't double-warn
	}
	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		d.add(check{
			Name:     "gh authenticated",
			Status:   statusWarn,
			Detail:   "run `gh auth login`",
			Required: false,
		})
		return
	}
	d.add(check{Name: "gh authenticated", Status: statusPass, Required: false})
}

func (d *doctor) checkGitRemote() {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	out, err := exec.Command("git", "-C", wd, "remote").Output()
	if err != nil {
		// Not a git repo — that's a separate failure mode the user will see
		// from `aios init` itself; doctor should not double-blame.
		return
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		d.add(check{
			Name:     "git remote configured",
			Status:   statusWarn,
			Detail:   "needed for `aios autopilot` and `aios serve`; add with `git remote add origin …`",
			Required: false,
		})
		return
	}
	d.add(check{Name: "git remote configured", Status: statusPass, Detail: strings.TrimSpace(string(out)), Required: false})
}

func (d *doctor) checkConfig() *config.Config {
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	p := filepath.Join(wd, ".aios", "config.toml")
	cfg, err := config.Load(p)
	if err != nil {
		d.add(check{
			Name:     ".aios/config.toml valid",
			Status:   statusWarn,
			Detail:   "run `aios init` first (" + err.Error() + ")",
			Required: false,
		})
		return nil
	}
	d.add(check{Name: ".aios/config.toml valid", Status: statusPass, Detail: fmt.Sprintf("schema v%d", cfg.SchemaVersion), Required: false})
	return cfg
}

func (d *doctor) checkEngineSmoke(ctx context.Context, name string, cfg *config.Config) {
	binary := name
	if cfg != nil {
		switch name {
		case "claude":
			if cfg.Engines.Claude.Binary != "" {
				binary = cfg.Engines.Claude.Binary
			}
		case "codex":
			if cfg.Engines.Codex.Binary != "" {
				binary = cfg.Engines.Codex.Binary
			}
		}
	}
	// Skip the smoke if the binary check already failed — exec'ing it would
	// just produce a redundant FAIL.
	if _, err := exec.LookPath(binary); err != nil {
		return
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, d.smokeTimeout)
	defer cancel()

	var eng engine.Engine
	switch name {
	case "claude":
		eng = &engine.ClaudeEngine{Binary: binary, TimeoutSec: int(d.smokeTimeout.Seconds())}
	case "codex":
		eng = &engine.CodexEngine{Binary: binary, TimeoutSec: int(d.smokeTimeout.Seconds())}
	default:
		return
	}
	resp, err := eng.Invoke(timeoutCtx, engine.InvokeRequest{
		Role:   engine.RoleCoder,
		Prompt: "respond with the single word OK and nothing else.",
	})
	if err != nil {
		d.add(check{
			Name:     name + " smoke call",
			Status:   statusFail,
			Detail:   err.Error(),
			Required: true,
		})
		return
	}
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		d.add(check{
			Name:     name + " smoke call",
			Status:   statusFail,
			Detail:   "empty response (auth missing? rate-limited?)",
			Required: true,
		})
		return
	}
	d.add(check{
		Name:     name + " smoke call",
		Status:   statusPass,
		Detail:   fmt.Sprintf("%d tokens, response: %q", resp.UsageTokens, truncate(text, 40)),
		Required: true,
	})
}

// report writes a human-readable table to d.out. Returns true when no
// required check failed.
func (d *doctor) report() bool {
	// Stable order: keep insertion order for predictable output.
	ok := true
	maxName := 0
	for _, c := range d.checks {
		if len(c.Name) > maxName {
			maxName = len(c.Name)
		}
	}
	fmt.Fprintln(d.out, "aios doctor")
	fmt.Fprintln(d.out, strings.Repeat("─", 78))
	for _, c := range d.checks {
		pad := strings.Repeat(" ", maxName-len(c.Name))
		line := fmt.Sprintf("[%s] %s%s", c.Status.tag(), c.Name, pad)
		if c.Detail != "" {
			line += "  " + c.Detail
		}
		fmt.Fprintln(d.out, line)
		if c.Required && c.Status == statusFail {
			ok = false
		}
	}
	fmt.Fprintln(d.out, strings.Repeat("─", 78))
	pass, warn, fail := 0, 0, 0
	for _, c := range d.checks {
		switch c.Status {
		case statusPass:
			pass++
		case statusWarn:
			warn++
		case statusFail:
			fail++
		}
	}
	fmt.Fprintf(d.out, "%d pass · %d warn · %d fail\n", pass, warn, fail)
	if ok && warn == 0 {
		fmt.Fprintln(d.out, "ready: all required checks passed.")
	} else if ok {
		fmt.Fprintln(d.out, "ready for `aios run`; warnings only affect autopilot/serve.")
	} else {
		fmt.Fprintln(d.out, "blocked: fix FAIL rows above before running aios.")
	}
	return ok
}

// parseGitVersion accepts strings like "git version 2.39.1" or
// "git version 2.42.0.windows.1" and returns the major and minor numbers.
func parseGitVersion(s string) (maj, min int, ok bool) {
	const prefix = "git version "
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return 0, 0, false
	}
	rest := s[idx+len(prefix):]
	parts := strings.SplitN(rest, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	for _, p := range []string{parts[0], parts[1]} {
		if p == "" {
			return 0, 0, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return 0, 0, false
			}
		}
	}
	maj = atoiSafe(parts[0])
	min = atoiSafe(parts[1])
	return maj, min, true
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
