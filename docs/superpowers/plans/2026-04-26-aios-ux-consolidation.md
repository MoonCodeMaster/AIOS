# AIOS UX Consolidation + Ship Pipeline — Implementation Plan (Plan 1 of 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Reshape the AIOS CLI to feel exactly like Claude CLI / Codex CLI — bare `aios` is a REPL, `aios "prompt"` is one-shot spec, `aios --ship "prompt"` is one-shot full automation. Delete the legacy `aios new`, `aios autopilot`, `aios architect` command surfaces. Preserve every underlying capability (specgen, decompose, coder/reviewer loop, PR/CI/merge, serve) by routing them through one shared `ShipPrompt` / `ShipSpec` helper used by root `--ship`, REPL `/ship`, and `serve`.

**Non-goals (deferred to later plans):**
- Plan 2: repackage `internal/architect/` as an internal complex-task planner (kept on disk untouched in Plan 1).
- Plan 3: repo-context ingestion, spec quality gate, intake stage, adaptive re-planning.
- Plan 4: comparative evals vs Claude CLI / Codex CLI.

**Architecture:** New `internal/cli/spectasks.go` owns the rehomed helpers (`writeTaskFiles`, `extractTaskID`, `commitSpec`, `decomposeOnly`) plus the new `ShipPrompt(ctx, wd, prompt, claude, codex) (ShipResult, error)` and `ShipSpec(ctx, wd) (ShipResult, error)` functions. Root command grows positional handling for `aios "prompt"`, plus `--ship` and `-p` flags. REPL's `runAutopilotShip` becomes a thin wrapper around `ShipSpec`. Serve's `subprocessAutopilot` is deleted in favor of in-process `ShipPrompt`.

**Tech Stack:** Go 1.21, cobra, existing engine/run/specgen packages.

**Spec / direction:** This conversation thread (no separate spec doc — design was iterated in dialogue and locked in user message confirming the four-plan split).

---

## Task 1: Rehome helpers to `internal/cli/spectasks.go`

**Files:**
- Create: `internal/cli/spectasks.go`
- Modify: `internal/cli/new.go` (remove the rehomed helpers; leave only `newNewCmd`/`runNew`/`NewOpts` which Task 8 deletes)
- Modify: `internal/cli/repl.go` (remove `decomposeOnly`, `runAutopilotShip`, `loadConfigForWd`, `promptsRender` — they move too)

- [ ] **Step 1: Create `internal/cli/spectasks.go`**

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
)

// writeTaskFiles parses a decompose-prompt response (===TASK=== separated)
// and writes one .md file per task under tasksDir. Returns the count written.
func writeTaskFiles(dir, raw string) (int, error) {
	parts := strings.Split(raw, "\n===TASK===\n")
	count := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id := extractTaskID(p)
		if id == "" {
			continue
		}
		path := filepath.Join(dir, id+".md")
		if err := os.WriteFile(path, []byte(p+"\n"), 0o644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// extractTaskID pulls the `id:` field from a task frontmatter block.
func extractTaskID(frontmatter string) string {
	for _, ln := range strings.Split(frontmatter, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "id:"))
		}
	}
	return ""
}

// commitSpec stashes any uncommitted edits, switches to the staging branch,
// stages .aios/, and commits with a one-line message describing the source.
// (Renamed from commitNewSpec — no longer "new"-specific.)
func commitSpec(wd, staging, source string) error {
	stash := exec.Command("git", "-C", wd, "stash", "-u")
	_ = stash.Run()
	chk := exec.Command("git", "-C", wd, "checkout", staging)
	if err := chk.Run(); err != nil {
		return err
	}
	add := exec.Command("git", "-C", wd, "add", ".aios")
	if err := add.Run(); err != nil {
		return err
	}
	msg := "aios: spec and tasks for " + source
	return exec.Command("git", "-C", wd, "commit", "-m", msg).Run()
}

// decomposeOnly turns the existing .aios/project.md into task files via
// codex's decompose prompt, writes them under .aios/tasks/, and commits
// the result to the staging branch. Used by both ShipSpec and the REPL.
func decomposeOnly(wd string) error {
	specPath := filepath.Join(wd, ".aios", "project.md")
	specBody, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read project.md: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return err
	}
	codex := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	dPrompt, err := prompts.Render("decompose.tmpl", map[string]string{"Spec": string(specBody)})
	if err != nil {
		return err
	}
	dRes, err := codex.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return err
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	if _, err := writeTaskFiles(tasksDir, dRes.Text); err != nil {
		return err
	}
	return commitSpec(wd, cfg.Project.StagingBranch, "interactive session")
}
```

- [ ] **Step 2: Remove rehomed helpers from `internal/cli/new.go`**

Edit `internal/cli/new.go` — delete the now-duplicate `writeTaskFiles`, `extractTaskID`, and the rename `commitNewSpec` → `commitSpec` (so the existing call inside `runNew` references the new name). After the edit, `new.go` should contain only `newNewCmd`, `NewOpts`, `runNew`, and any imports those need. Replace `commitNewSpec(...)` callsite with `commitSpec(...)`.

- [ ] **Step 3: Remove rehomed helpers from `internal/cli/repl.go`**

Delete `decomposeOnly`, `loadConfigForWd`, and `promptsRender` from `repl.go`. The `runAutopilotShip` function stays for now (Task 3 rewires it to use `ShipSpec`).

- [ ] **Step 4: Verify build and tests**

```
go build ./...
go test -race ./internal/cli/...
```

Expected: clean build; all existing CLI tests pass (the rehome is pure code motion, no behavior change).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/spectasks.go internal/cli/new.go internal/cli/repl.go
git commit -m "refactor(cli): rehome spec/task helpers into spectasks.go"
```

---

## Task 2: Add `ShipPrompt`, `ShipSpec`, and `ShipResult`

**Files:**
- Modify: `internal/cli/spectasks.go` (add the new functions and types)
- Create: `internal/cli/spectasks_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/spectasks_test.go`:

```go
package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestShipPromptWritesSpecThenShips(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	called := false
	res, err := ShipPrompt(context.Background(), ShipPromptInput{
		Wd: wd, Prompt: "build a thing", Claude: claude, Codex: codex,
		ShipSpecFn: func(_ context.Context, w string) (ShipResult, error) {
			data, err := os.ReadFile(filepath.Join(w, ".aios", "project.md"))
			if err != nil {
				return ShipResult{}, err
			}
			if string(data) != "POLISHED" {
				t.Fatalf("ShipSpec saw spec %q, want POLISHED", data)
			}
			called = true
			return ShipResult{Status: ShipMerged, PRURL: "https://example/pr/1", PRNumber: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("ShipPrompt: %v", err)
	}
	if !called {
		t.Fatalf("ShipSpecFn was not called")
	}
	if res.Status != ShipMerged || res.PRNumber != 1 {
		t.Fatalf("Result = %+v", res)
	}
}

func TestShipPromptSpecgenError(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &errEngineCli{err: errors.New("claude offline")}
	codex := &errEngineCli{err: errors.New("codex offline")}
	_, err := ShipPrompt(context.Background(), ShipPromptInput{
		Wd: wd, Prompt: "x", Claude: claude, Codex: codex,
	})
	if err == nil {
		t.Fatalf("expected error when both drafters fail")
	}
	// Spec must NOT have been written when specgen fails.
	if _, statErr := os.Stat(filepath.Join(wd, ".aios", "project.md")); !os.IsNotExist(statErr) {
		t.Fatalf("project.md should not exist after specgen failure; stat err = %v", statErr)
	}
}

// errEngineCli is a local error-only Engine for cli-package tests.
type errEngineCli struct{ err error }

func (e *errEngineCli) Name() string { return "errEngineCli" }
func (e *errEngineCli) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	return nil, e.err
}
```

- [ ] **Step 2: Verify tests fail**

`go test ./internal/cli/ -run TestShip` — expect compile failure (ShipPrompt, ShipSpec, ShipPromptInput, ShipResult, ShipMerged not defined).

- [ ] **Step 3: Add the types and functions to `internal/cli/spectasks.go`**

Append to `spectasks.go`:

```go
import (
	// add to existing imports:
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
	"time"
)

// ShipStatus is the outcome of a ship pipeline run.
type ShipStatus int

const (
	ShipUnknown ShipStatus = iota
	ShipMerged
	ShipPRRed
	ShipAbandoned
)

// ShipResult is the structured outcome of one ShipSpec or ShipPrompt run.
type ShipResult struct {
	Status     ShipStatus
	PRURL      string
	PRNumber   int
	AuditTrail string
}

// ShipPromptInput bundles the inputs to ShipPrompt. Engines and the
// (optional) ShipSpecFn override are injectable for tests.
type ShipPromptInput struct {
	Wd         string
	Prompt     string
	Claude     engine.Engine
	Codex      engine.Engine
	ShipSpecFn func(ctx context.Context, wd string) (ShipResult, error) // nil = use real ShipSpec
	OnStage    func(name string)                                        // optional progress callback for specgen stages
}

// ShipPrompt runs specgen.Generate on the prompt, writes the polished
// spec to .aios/project.md, then calls ShipSpec to decompose+execute.
// On specgen failure, project.md is NOT written and the error is returned.
func ShipPrompt(ctx context.Context, in ShipPromptInput) (ShipResult, error) {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(in.Wd, ".aios", "runs"), runID)
	if err != nil {
		return ShipResult{}, fmt.Errorf("open run dir: %w", err)
	}
	out, err := specgen.Generate(ctx, specgen.Input{
		UserRequest:  in.Prompt,
		Claude:       in.Claude,
		Codex:        in.Codex,
		Recorder:     rec,
		OnStageStart: in.OnStage,
	})
	if err != nil {
		return ShipResult{}, fmt.Errorf("specgen: %w", err)
	}
	specPath := filepath.Join(in.Wd, ".aios", "project.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		return ShipResult{}, err
	}
	if err := os.WriteFile(specPath, []byte(out.Final), 0o644); err != nil {
		return ShipResult{}, err
	}
	if in.ShipSpecFn != nil {
		return in.ShipSpecFn(ctx, in.Wd)
	}
	return ShipSpec(ctx, in.Wd)
}

// ShipSpec decomposes the existing .aios/project.md into task files,
// then runs `aios run --autopilot --merge` in-process. Returns a
// structured result parsed from the run's autopilot-summary.md.
func ShipSpec(ctx context.Context, wd string) (ShipResult, error) {
	if err := decomposeOnly(wd); err != nil {
		return ShipResult{}, fmt.Errorf("decompose: %w", err)
	}
	runCmd := newRunCmd()
	if err := runCmd.Flags().Set("autopilot", "true"); err != nil {
		return ShipResult{}, fmt.Errorf("set --autopilot: %w", err)
	}
	if err := runCmd.Flags().Set("merge", "true"); err != nil {
		return ShipResult{}, fmt.Errorf("set --merge: %w", err)
	}
	if err := runMain(runCmd, nil); err != nil {
		// Even on error, parse the summary if it landed.
		if res, perr := parseLatestShipSummary(wd); perr == nil {
			return res, err
		}
		return ShipResult{}, err
	}
	return parseLatestShipSummary(wd)
}

// parseLatestShipSummary reads the autopilot-summary.md from the most
// recently created .aios/runs/<id>/ directory and parses it into a
// ShipResult. Same parser shape as the previous serve-side parser.
func parseLatestShipSummary(wd string) (ShipResult, error) {
	runsDir := filepath.Join(wd, ".aios", "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return ShipResult{}, fmt.Errorf("read runs dir: %w", err)
	}
	var newest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > newest {
			newest = e.Name()
		}
	}
	if newest == "" {
		return ShipResult{}, fmt.Errorf("no runs found under %s", runsDir)
	}
	body, err := os.ReadFile(filepath.Join(runsDir, newest, "autopilot-summary.md"))
	if err != nil {
		return ShipResult{}, fmt.Errorf("read autopilot-summary.md: %w", err)
	}
	return parseShipSummary(string(body))
}

func parseShipSummary(body string) (ShipResult, error) {
	res := ShipResult{Status: ShipUnknown}
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "PR: "):
			res.PRURL = strings.TrimPrefix(ln, "PR: ")
			parts := strings.Split(res.PRURL, "/")
			if len(parts) > 0 {
				_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &res.PRNumber)
			}
		case strings.HasPrefix(ln, "Merged: true"):
			res.Status = ShipMerged
		case strings.HasPrefix(ln, "Merged: false"):
			res.Status = ShipPRRed
		case strings.Contains(ln, "all tasks abandoned") || strings.Contains(ln, "Skipped: no converged tasks"):
			res.Status = ShipAbandoned
			res.AuditTrail = body
		}
	}
	if res.Status == ShipUnknown {
		return res, fmt.Errorf("autopilot-summary.md did not yield a recognised status:\n%s", body)
	}
	return res, nil
}
```

- [ ] **Step 4: Run tests**

`go test -race ./internal/cli/ -run TestShip -v` — both tests pass.
`go test -race ./...` — no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/spectasks.go internal/cli/spectasks_test.go
git commit -m "feat(cli): ShipPrompt + ShipSpec — shared automation contract"
```

---

## Task 3: Rewire REPL `/ship` to use `ShipSpec`

**Files:**
- Modify: `internal/cli/repl.go`

- [ ] **Step 1: Replace `runAutopilotShip` body**

In `repl.go`, replace the body of `runAutopilotShip` so it just calls `ShipSpec`:

```go
func runAutopilotShip(ctx context.Context, wd string) error {
	_, err := ShipSpec(ctx, wd)
	return err
}
```

(Remove the inline decompose+runCmd block. The Repl's `ShipFn` callsite is unchanged — it still receives `(ctx, wd)`.)

- [ ] **Step 2: Verify all REPL tests still pass**

`go test -race ./internal/cli/ -run TestRepl -v` — TestReplShipCallsAutopilotHook and the integration tests still pass (they inject `ShipFn` to bypass the real impl).

- [ ] **Step 3: Commit**

```bash
git add internal/cli/repl.go
git commit -m "refactor(repl): /ship now delegates to ShipSpec helper"
```

---

## Task 4: Rewire `aios serve` to use `ShipPrompt` in-process

**Files:**
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/serve_runner.go` (only if AutopilotFn signature needs adjustment)

- [ ] **Step 1: Read the current serve callsite to understand the existing AutopilotFn signature**

`grep -n "AutopilotFn\|subprocessAutopilot" internal/cli/serve.go internal/cli/serve_runner.go` — confirm the function shape and how AutopilotResult is consumed.

- [ ] **Step 2: Replace `subprocessAutopilot` with an in-process ShipPrompt call**

In `internal/cli/serve.go`, delete `subprocessAutopilot`, `snapshotRunIDs`, `newestNew`, and `parseAutopilotSummary` (all dead after this task). Replace the AutopilotFn wiring so it calls a new `inProcessShip` function:

```go
// inProcessShip runs the new ship pipeline for one issue body. Replaces
// the prior subprocess-out-to-`aios autopilot` path.
func inProcessShip(ctx context.Context, idea string) (AutopilotResult, error) {
	wd, err := os.Getwd()
	if err != nil {
		return AutopilotResult{}, err
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return AutopilotResult{}, fmt.Errorf("load config: %w", err)
	}
	claude := &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec}
	codex := &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec}
	res, err := ShipPrompt(ctx, ShipPromptInput{
		Wd: wd, Prompt: idea, Claude: claude, Codex: codex,
	})
	return shipResultToAutopilot(res), err
}

// shipResultToAutopilot bridges the new ShipResult type to the existing
// AutopilotResult/AutopilotStatus types still used by serve_runner.go.
func shipResultToAutopilot(s ShipResult) AutopilotResult {
	out := AutopilotResult{PRURL: s.PRURL, PRNumber: s.PRNumber, AuditTrail: s.AuditTrail}
	switch s.Status {
	case ShipMerged:
		out.Status = AutopilotMerged
	case ShipPRRed:
		out.Status = AutopilotPRRed
	case ShipAbandoned:
		out.Status = AutopilotAbandoned
	default:
		out.Status = AutopilotUnknown
	}
	return out
}
```

Update the AutopilotFn assignment in serve.go's `runServe` (or wherever it's wired) to use `inProcessShip` instead of `subprocessAutopilot`.

Add the new imports: `context`, `github.com/MoonCodeMaster/AIOS/internal/config`, `github.com/MoonCodeMaster/AIOS/internal/engine`. Drop the now-unused `os/exec` import if no other callsites need it.

**Note:** AutopilotResult and AutopilotStatus stay defined in serve_runner.go for now. Plan 2 may unify them with ShipResult, but Plan 1 does the minimal bridge.

- [ ] **Step 3: Verify serve tests still pass**

`go test -race ./internal/cli/ -run TestServe -v` — the existing serve tests inject AutopilotFn directly via the test harness, so they should be unaffected.

`go test -race ./...` — no regressions.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/serve.go internal/cli/serve_runner.go
git commit -m "refactor(serve): in-process ShipPrompt replaces subprocess autopilot"
```

---

## Task 5: Add `aios "prompt"` one-shot spec mode

**Files:**
- Modify: `internal/cli/root.go`
- Create: `internal/cli/oneshot.go`
- Create: `internal/cli/oneshot_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/oneshot_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestOneShotSpecWritesProjectMd(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED_FINAL"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	stdout := &bytes.Buffer{}
	err := runOneShot(context.Background(), OneShotInput{
		Wd: wd, Prompt: "build a thing", Claude: claude, Codex: codex, Out: stdout,
	})
	if err != nil {
		t.Fatalf("runOneShot: %v", err)
	}
	specBody, err := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if err != nil {
		t.Fatalf("read project.md: %v", err)
	}
	if string(specBody) != "POLISHED_FINAL" {
		t.Fatalf("project.md = %q, want POLISHED_FINAL", specBody)
	}
}
```

- [ ] **Step 2: Verify test fails**

`go test ./internal/cli/ -run TestOneShotSpec` — expect compile failure (`runOneShot`, `OneShotInput` undefined).

- [ ] **Step 3: Create `internal/cli/oneshot.go`**

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
)

// OneShotInput bundles the inputs to a non-interactive single-prompt
// invocation of `aios "prompt"`. Engines are injectable for tests.
type OneShotInput struct {
	Wd     string
	Prompt string
	Claude engine.Engine
	Codex  engine.Engine
	Out    io.Writer
}

// runOneShot runs specgen on the prompt, writes the polished spec to
// .aios/project.md, and prints a brief confirmation to Out. Used by
// `aios "prompt"` (no flags). Does NOT ship — that's `aios --ship`.
func runOneShot(ctx context.Context, in OneShotInput) error {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(in.Wd, ".aios", "runs"), runID)
	if err != nil {
		return fmt.Errorf("open run dir: %w", err)
	}
	fmt.Fprintln(in.Out, "running 4-stage pipeline…")
	out, err := specgen.Generate(ctx, specgen.Input{
		UserRequest: in.Prompt,
		Claude:      in.Claude,
		Codex:       in.Codex,
		Recorder:    rec,
		OnStageStart: func(name string) {
			fmt.Fprintf(in.Out, "  · %s …\n", name)
		},
	})
	if err != nil {
		return fmt.Errorf("specgen: %w", err)
	}
	specPath := filepath.Join(in.Wd, ".aios", "project.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(specPath, []byte(out.Final), 0o644); err != nil {
		return err
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(in.Out, "  ! %s\n", w)
	}
	fmt.Fprintf(in.Out, "Spec written to %s. Run `aios --ship \"%s\"` to implement, or open the REPL with `aios` to refine.\n", specPath, in.Prompt)
	return nil
}
```

- [ ] **Step 4: Wire in `internal/cli/root.go`**

Modify the root command's `RunE` to dispatch on positional args:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			ship, _ := cmd.Flags().GetBool("ship")
			print, _ := cmd.Flags().GetBool("print")
			resumeID, _ := cmd.Flags().GetString("continue")

			// Bare aios with no args → REPL.
			if len(args) == 0 {
				if ship || print {
					return fmt.Errorf("--ship and -p require a prompt argument")
				}
				return launchRepl(cmd.Context(), resumeID)
			}
			// aios "prompt" with positional → one-shot.
			prompt := strings.Join(args, " ")
			if ship && print {
				return fmt.Errorf("--ship and -p are mutually exclusive")
			}
			if resumeID != "" {
				return fmt.Errorf("--continue is REPL-only; do not combine with a prompt")
			}
			// (Task 6 wires --ship; Task 7 wires -p. Here only the no-flag path.)
			if ship || print {
				return fmt.Errorf("not implemented yet (Task 6 / 7)")
			}
			return launchOneShot(cmd.Context(), prompt)
		},
```

Add a `launchOneShot` helper near `launchRepl`:

```go
// launchOneShot boots real engines for `aios "prompt"`, runs runOneShot.
func launchOneShot(ctx context.Context, prompt string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	return runOneShot(ctx, OneShotInput{
		Wd:     wd,
		Prompt: prompt,
		Claude: &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:  &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		Out:    os.Stdout,
	})
}
```

Add the new flags on the root command (regular Flags, not PersistentFlags — they only apply to bare `aios`):

```go
	root.Flags().Bool("ship", false, "run the full ship pipeline: specgen + decompose + execute + PR + merge")
	root.Flags().BoolP("print", "p", false, "print the generated spec to stdout (no project.md write, no shipping)")
```

Add the `strings` import.

- [ ] **Step 5: Verify tests pass**

`go test -race ./internal/cli/ -v` — all REPL + new one-shot tests pass.
`go test -race ./...` — no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/oneshot.go internal/cli/oneshot_test.go internal/cli/root.go
git commit -m "feat(cli): aios \"prompt\" — one-shot spec generation, no ship"
```

---

## Task 6: Wire `aios --ship "prompt"` to ShipPrompt

**Files:**
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Replace the Task 5 stub for `--ship`**

In root.go's `RunE`, replace the `if ship || print { return errors.New("not implemented yet") }` block with:

```go
			if ship {
				_, err := launchShip(cmd.Context(), prompt)
				return err
			}
			if print {
				return fmt.Errorf("not implemented yet (Task 7)")
			}
```

Add `launchShip`:

```go
func launchShip(ctx context.Context, prompt string) (ShipResult, error) {
	wd, err := os.Getwd()
	if err != nil {
		return ShipResult{}, fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return ShipResult{}, fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	fmt.Fprintf(os.Stdout, "shipping %q…\n", prompt)
	return ShipPrompt(ctx, ShipPromptInput{
		Wd:     wd,
		Prompt: prompt,
		Claude: &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:  &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		OnStage: func(name string) { fmt.Fprintf(os.Stdout, "  · %s …\n", name) },
	})
}
```

- [ ] **Step 2: Add a test for the dispatch**

This is hard to test directly without a real autopilot run (which would shell out). Instead, verify the dispatcher chooses the right path with mocked flags. Append to `internal/cli/oneshot_test.go`:

```go
func TestRootDispatchValidatesFlagCombos(t *testing.T) {
	cases := []struct {
		args []string
		ship bool
		prn  bool
		cont string
		want string // substring of expected error, or "" for success
	}{
		{[]string{}, true, false, "", "require a prompt"},
		{[]string{}, false, true, "", "require a prompt"},
		{[]string{"X"}, true, true, "", "mutually exclusive"},
		{[]string{"X"}, false, false, "session-x", "REPL-only"},
	}
	for _, c := range cases {
		err := dispatchRoot(c.args, c.ship, c.prn, c.cont, nil, nil)
		if c.want == "" {
			if err != nil {
				t.Fatalf("args=%v: unexpected err %v", c.args, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("args=%v ship=%v print=%v cont=%q: want err containing %q, got %v", c.args, c.ship, c.prn, c.cont, c.want, err)
		}
	}
}
```

This requires extracting the validation logic into a testable `dispatchRoot` function. Refactor `RunE` so it calls `dispatchRoot(args, ship, print, resumeID, replFn, oneShotFn)` where `replFn` and `oneShotFn` are injectable. The test can pass nil for those — only the validation paths should be exercised.

(The implementer may need to adjust the exact signature; the goal is testable validation.)

- [ ] **Step 3: Verify**

`go test -race ./internal/cli/ -v` — passes.
`go build ./...` — clean.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go internal/cli/oneshot_test.go
git commit -m "feat(cli): aios --ship \"prompt\" — full automation in one command"
```

---

## Task 7: Wire `aios -p "prompt"` to print-only mode

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/oneshot.go`
- Modify: `internal/cli/oneshot_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/oneshot_test.go`:

```go
func TestPrintModeWritesToStdoutOnlyNoFile(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED_PRINT"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	stdout := &bytes.Buffer{}
	err := runPrintMode(context.Background(), PrintModeInput{
		Wd: wd, Prompt: "x", Claude: claude, Codex: codex, Out: stdout,
	})
	if err != nil {
		t.Fatalf("runPrintMode: %v", err)
	}
	if stdout.String() != "POLISHED_PRINT\n" && stdout.String() != "POLISHED_PRINT" {
		t.Fatalf("stdout = %q, want exactly POLISHED_PRINT", stdout.String())
	}
	// project.md must NOT exist.
	if _, err := os.Stat(filepath.Join(wd, ".aios", "project.md")); !os.IsNotExist(err) {
		t.Fatalf("project.md should not exist in print mode; stat err = %v", err)
	}
}
```

- [ ] **Step 2: Verify it fails**

`go test ./internal/cli/ -run TestPrintMode` — expect compile failure.

- [ ] **Step 3: Add `runPrintMode` to `oneshot.go`**

Append to `oneshot.go`:

```go
// PrintModeInput bundles the inputs for `aios -p "prompt"`.
type PrintModeInput struct {
	Wd     string
	Prompt string
	Claude engine.Engine
	Codex  engine.Engine
	Out    io.Writer
}

// runPrintMode runs specgen and writes ONLY the polished spec to Out.
// No project.md, no progress noise, no run dir summary on stdout.
// Audit artifacts under .aios/runs/<id>/specgen/ still get written
// (the Recorder is bound) so debugging is preserved.
func runPrintMode(ctx context.Context, in PrintModeInput) error {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(in.Wd, ".aios", "runs"), runID)
	if err != nil {
		return fmt.Errorf("open run dir: %w", err)
	}
	out, err := specgen.Generate(ctx, specgen.Input{
		UserRequest: in.Prompt,
		Claude:      in.Claude,
		Codex:       in.Codex,
		Recorder:    rec,
	})
	if err != nil {
		return fmt.Errorf("specgen: %w", err)
	}
	_, err = fmt.Fprint(in.Out, out.Final)
	return err
}
```

- [ ] **Step 4: Wire in root.go**

In root.go's `RunE`, replace the `print` stub with:

```go
			if print {
				return launchPrintMode(cmd.Context(), prompt)
			}
```

Add `launchPrintMode`:

```go
func launchPrintMode(ctx context.Context, prompt string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	return runPrintMode(ctx, PrintModeInput{
		Wd:     wd,
		Prompt: prompt,
		Claude: &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:  &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		Out:    os.Stdout,
	})
}
```

- [ ] **Step 5: Verify**

`go test -race ./internal/cli/ -v` — TestPrintMode passes.
`go build ./...` clean.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/oneshot.go internal/cli/oneshot_test.go internal/cli/root.go
git commit -m "feat(cli): aios -p \"prompt\" — print spec to stdout, no side effects"
```

---

## Task 8: Delete `aios new` command and its tests

**Files:**
- Delete: `internal/cli/new.go`
- Delete: `internal/cli/new_test.go`
- Modify: `internal/cli/root.go` (remove `root.AddCommand(newNewCmd())`)

- [ ] **Step 1: Confirm new.go is now down to just newNewCmd / NewOpts / runNew**

`cat internal/cli/new.go` — verify only the cobra wrapper + the orchestration function remain (helpers were rehomed in Task 1).

- [ ] **Step 2: Delete the files**

```bash
git rm internal/cli/new.go internal/cli/new_test.go
```

- [ ] **Step 3: Remove the registration from root.go**

Delete the line `root.AddCommand(newNewCmd())`.

- [ ] **Step 4: Verify build**

`go build ./...` — clean. No callers should remain (Task 1 removed all helper duplicates; nobody else calls `runNew` because Task 9 will delete autopilot which was the last caller).

If autopilot.go still calls `runNew`, wait — Task 9 will delete autopilot.go and that callsite goes away. But the build needs to be clean BETWEEN tasks. So Task 8 and Task 9 must commit together, OR the order needs flipping.

**Order fix:** Do Task 9 (delete autopilot) BEFORE Task 8 (delete new). Or commit them in the same task. **Recommendation:** merge Tasks 8+9+10 into one deletion task — they all need to land atomically because they cross-reference (autopilot calls runNew; root.go references all three).

→ See Task 8 (revised) below.

---

## Task 8 (revised): Delete `aios new`, `aios autopilot`, `aios architect` together

**Files:**
- Delete: `internal/cli/new.go`, `internal/cli/new_test.go`
- Delete: `internal/cli/autopilot.go`, `internal/cli/autopilot_test.go`, `internal/cli/run_autopilot_test.go`
- Delete: `internal/cli/preflight_autopilot.go`, `internal/cli/preflight_autopilot_test.go`
- Delete: `internal/cli/architect.go`, `internal/cli/architect_test.go`
- Modify: `internal/cli/root.go` (remove three AddCommand calls)
- KEEP: `internal/architect/` package (Plan 2 reshapes it)

- [ ] **Step 1: Delete the files**

```bash
git rm internal/cli/new.go internal/cli/new_test.go \
       internal/cli/autopilot.go internal/cli/autopilot_test.go internal/cli/run_autopilot_test.go \
       internal/cli/preflight_autopilot.go internal/cli/preflight_autopilot_test.go \
       internal/cli/architect.go internal/cli/architect_test.go
```

- [ ] **Step 2: Remove three AddCommand calls from root.go**

Delete:
- `root.AddCommand(newNewCmd())`
- `root.AddCommand(newAutopilotCmd())`
- `root.AddCommand(newArchitectCmd())`

- [ ] **Step 3: Verify build**

`go build ./...` — clean.

If anything in `internal/cli/` still references symbols from the deleted files (e.g. some prompt or helper that was only used by autopilot), the build will tell you. Move that helper into `spectasks.go` or delete it.

`go test -race ./...` — all remaining tests pass.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "remove(cli): aios new, autopilot, architect — replaced by aios + --ship"
```

---

## Task 9: Update README and architecture doc

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`

- [ ] **Step 1: Update README**

Major rewrites:
- Command index: remove `aios new`, `aios autopilot`, `aios architect` rows. Add `aios "prompt"` and `aios --ship "prompt"` rows.
- Quick start section: replace any `aios new` examples with the new surface.
- Per-command sections: delete the dedicated sections for the three removed commands.
- Interactive mode section (already present from earlier work): keep mostly as-is, update any cross-references.
- Add a new top-level section "Three ways to drive AIOS" or similar:

```markdown
## Three ways to drive AIOS

```
aios                       # interactive REPL — talk, refine, /ship when ready
aios "build X"             # one-shot spec → .aios/project.md, no execution
aios --ship "build X"      # full pipeline: specgen → decompose → execute → PR → merge
```

All three run the same 4-stage dual-AI pipeline (Claude draft + Codex draft → Codex merge → Claude polish). The difference is what happens after the spec lands.
```

The voice should match existing terse natural-personal style — no marketing copy, no AI vocabulary like "leverages" or "seamless." Look at existing `aios review` / `aios duel` sections for tone.

- [ ] **Step 2: Update architecture doc**

In `docs/architecture.md`:
- Remove any references to `aios new`, `aios autopilot`, `aios architect` as discrete commands.
- Add a "Ship pipeline" subsection describing the shared `ShipPrompt` / `ShipSpec` flow used by root `--ship`, REPL `/ship`, and serve.
- Update the system overview diagram if any.

- [ ] **Step 3: Verify the docs render cleanly**

`head -100 README.md` — confirm structure is intact.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/architecture.md
git commit -m "docs: rewrite for aios / aios \"prompt\" / aios --ship surface"
```

---

## Task 10: Update e2e test in `test/e2e/greenfield_test.go`

**Files:**
- Modify: `test/e2e/greenfield_test.go`

- [ ] **Step 1: Read the existing test**

`cat test/e2e/greenfield_test.go` — confirm what it does (currently: `aios init` → `aios new` → `aios run`).

- [ ] **Step 2: Replace the `aios new` invocation with `aios "prompt"` then `aios run`**

Two reasonable rewrites:
1. **Minimal change:** Replace `aios new "..."` with `aios "..."` (the new one-shot spec mode). Keep the subsequent `aios run`. This tests the same end-to-end behavior with the new surface.
2. **Full new path:** Replace both `aios new "..."` and `aios run` with a single `aios --ship "..."`. This exercises the new ship contract end-to-end.

Pick **option 1** to preserve the test's original intent (decoupled spec then run) AND minimize risk. A separate Plan 1 follow-up test could exercise `--ship` end-to-end.

The line:
```go
cmd = exec.Command(aios, "new", "Build a CLI that reverses its argv, with unit tests")
cmd.Stdin = strings.NewReader("y\n")
```
becomes:
```go
cmd = exec.Command(aios, "Build a CLI that reverses its argv, with unit tests")
// no stdin needed — one-shot spec mode does not prompt
```

- [ ] **Step 3: Verify the e2e still builds**

`go build ./test/e2e/...` — clean.

(Running the test itself requires real Claude+Codex CLIs and is gated by build tags / manual invocation, so don't run it in CI here.)

- [ ] **Step 4: Commit**

```bash
git add test/e2e/greenfield_test.go
git commit -m "test(e2e): greenfield uses aios \"prompt\" instead of aios new"
```

---

## Task 11: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Build the binary**

```bash
cd ~/.config/superpowers/worktrees/AIOS/feat-aios-interactive-specgen-2026-04-26
go build -o bin/aios ./cmd/aios
```
Expected: clean build.

- [ ] **Step 2: Full test suite under `-race`**

```bash
go test -race ./...
```
Expected: all packages pass. (TestRebaseReviewRejects is known flaky — retry once if it fails.)

- [ ] **Step 3: Help output smoke test**

```bash
./bin/aios --help
```
Expected:
- `Usage: aios [flags]` and `aios [command]` shown.
- `--ship`, `-p` (`--print`), `--continue` listed in the flag section.
- The subcommand list includes `init`, `doctor`, `cost`, `lessons`, `review`, `mcp`, `status`, `serve`, `run`, `resume` — but NOT `new`, `autopilot`, `architect`.

- [ ] **Step 4: Per-mode smoke (manual, no API hits — just argv parsing)**

```bash
./bin/aios --ship -p "X" 2>&1 | grep "mutually exclusive"  # expect match
./bin/aios --ship 2>&1 | grep "require a prompt"           # expect match
./bin/aios --continue "X" 2>&1 | grep "REPL-only"          # expect match
```

- [ ] **Step 5: Commit any final tweaks**

If any smoke test exposed a bug, fix and commit. Otherwise this task is verification-only.

---

## Self-review

**Spec coverage:** every revision the user asked for in the most recent message is mapped to a task:
- New surface (`aios`, `aios "prompt"`, `aios --ship`, `aios -p`, `aios --continue`) — Tasks 5, 6, 7 (REPL + `--continue` already exist from Plan 0).
- Shared ship pipeline used by root, REPL, serve — Tasks 2, 3, 4.
- Rehome helpers to spectasks.go — Task 1.
- Replace `runNew` with `specgen.Generate` — Task 2 (`ShipPrompt` calls specgen.Generate; `runNew` deleted in Task 8).
- Rewire serve through ship helper, not subprocess — Task 4.
- Keep `aios run` public — confirmed (no deletion task for it).
- `aios resume` → `aios run --unblock`: deferred (user said "only if it does not delay the core ship path"). NOT in Plan 1.
- Keep `bp-*.tmpl` for now — confirmed (no prompt deletion task in Plan 1).
- `-p` is stdout-only, no project.md write — Task 7.
- Architect package on disk — Task 8 only deletes the CLI wrapper, package preserved.

**Placeholder scan:** Task 5 has a stub for --ship that Task 6 replaces, and a stub for -p that Task 7 replaces. These are explicit and intentional (TDD progression). All other steps have concrete code.

**Type consistency:**
- `ShipResult`, `ShipStatus`, `ShipMerged`/`ShipPRRed`/`ShipAbandoned` defined in Task 2, used in Tasks 4, 6.
- `OneShotInput`, `PrintModeInput` defined in Tasks 5, 7.
- `commitSpec` (renamed from `commitNewSpec`) used in Task 1's `decomposeOnly` and presumed callers.

**Risk tracking:**
- Task 2's `ShipSpec` calls `runMain(newRunCmd(), nil)`. This relies on `newRunCmd` and `runMain` continuing to exist in `internal/cli/run.go` — they do, and Plan 1 doesn't touch them.
- Task 4 deletes serve helpers. If a future task needs them back, they're in git history.
- Task 8 is the only "atomic" delete — it has to land the three command deletions plus `preflight_autopilot.*` together because they cross-reference. Worth flagging during execution.

**Out of scope (correctly deferred):**
- Plan 2: architect → internal complex-task planner.
- Plan 3: repo context, spec quality gate, intake stage, adaptive re-planning.
- Plan 4: comparative evals.
