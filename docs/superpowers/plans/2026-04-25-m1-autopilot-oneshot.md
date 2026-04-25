# M1 — One-shot Autopilot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `aios autopilot "<idea>"` — a single command that runs spec → tasks → coder↔reviewer → PR → CI poll → squash-merge to `main` with zero human input. Removes the two existing human gates (`aios new` confirm prompt; manual `git merge aios/staging`). Targets release **v0.2.0**.

**Architecture:** New `internal/githost` package (gh-CLI wrapper) + new `aios autopilot` Cobra command + `--auto`/`--autopilot`/`--merge` flags on existing commands + abandoned-task local drop replacing the `[NEEDS HUMAN]` stop. Autopilot finalizer composes the existing `aios new` and `aios run` flows without reimplementing them; the merge step happens via `gh pr create` → `gh pr checks` poll → `gh pr merge --squash --delete-branch`.

**Tech Stack:** Go 1.26.2, Cobra (CLI), `gh` CLI (GitHub auth + REST), TOML config, existing `internal/spec`/`internal/orchestrator`/`internal/run` packages. Tests use the existing `testing` package with `t.TempDir()` + fake-exec patterns already established in the codebase.

**Spec reference:** `docs/superpowers/specs/2026-04-25-autopilot-roadmap-design.md` § M1.

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `internal/githost/githost.go` | Public `Host` interface (`OpenPR`, `WaitForChecks`, `MergePR`) plus shared types (`PR`, `ChecksState`, `MergeMode`). |
| `internal/githost/cli.go` | Real implementation backed by the `gh` CLI. Single struct `CLIHost`. |
| `internal/githost/cli_test.go` | Unit tests for `CLIHost` using `exec.LookPath` indirection (matches existing engine test pattern). |
| `internal/githost/fake.go` | Deterministic in-memory `FakeHost` for integration tests. |
| `internal/githost/fake_test.go` | Tiny tests for the fake to keep its semantics stable. |
| `internal/run/abandoned.go` | `WriteAbandoned(rec *Recorder, taskID string, outcome *orchestrator.Outcome)` — writes `report.md` + `full-trail.json` under `.aios/runs/<id>/abandoned/<task>/`. |
| `internal/run/abandoned_test.go` | Unit tests for the writer (artifact layout + idempotence). |
| `internal/cli/autopilot.go` | `aios autopilot "<idea>"` Cobra command. Composes `runNewAuto` + `runAutopilotMerge`. |
| `internal/cli/preflight_autopilot.go` | Preflight checks specific to `--autopilot --merge` (gh on PATH, gh auth status, remote exists). Kept separate from the existing `preflight()` so it can be unit-tested without git fixtures. |
| `internal/cli/preflight_autopilot_test.go` | Unit tests using `exec.LookPath` indirection. |
| `test/integration/autopilot_oneshot_test.go` | End-to-end happy path with fake engines + fake githost. |
| `test/integration/autopilot_abandoned_test.go` | Stuck-task drop+continue path. |
| `test/integration/autopilot_ci_red_test.go` | CI-red path: PR opened but never merged. |

**Modified files:**

| Path | Change |
|---|---|
| `internal/cli/new.go` | Extract body into `runNew(opts NewOpts) error`; add `--auto` flag (skips confirm gate when true). Existing interactive `aios new` calls `runNew` with `Auto: false`. |
| `internal/cli/run.go` | Extract body into `runImpl(opts RunOpts) error`; add `--autopilot` and `--merge` flags. `--autopilot` swaps the abandon path; `--merge` triggers the finalizer. The existing `runMain` shim parses flags and calls `runImpl`. |
| `internal/cli/root.go` | Register `newAutopilotCmd()` on the root command. |

**No changes needed (already supports M1 requirements):**
- `internal/spec/task.go` — `Status` is a free-form string; `"abandoned"` parses without modification.
- `internal/config/config.go` — `Budget.StallThreshold` already wired through `Deps.StallThreshold` (`run.go:317`), with `defaultStallThreshold = 3` fallback in the orchestrator. The 2026-04-24 painpoints memory note about P1-5 is stale.
- `internal/orchestrator/*` — abandoned-task drop is a CLI-layer change; the orchestrator still returns `StateBlocked` with `CodeStallNoProgress`. The CLI decides whether that block becomes "abandoned and continue" (autopilot) or "block and stop" (legacy).

---

## Implementation order

The plan is **TDD throughout**: every task starts with a failing test, makes it pass with the minimum code, then commits. Order respects dependencies:

1. Tasks 1–4 build `internal/githost` bottom-up (interface → real impl → fake).
2. Tasks 5–6 add the CLI flag plumbing and the abandoned-writer (independent of githost).
3. Tasks 7–9 wire it together: refactor `aios run`, add finalizer, add preflight.
4. Task 10 adds the `aios autopilot` command on top of all the above.
5. Tasks 11–13 are end-to-end integration tests.
6. Task 14 is README + release-notes prep.

---

## Task 1: Define the `githost.Host` interface

**Files:**
- Create: `internal/githost/githost.go`
- Test: `internal/githost/githost_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/githost/githost_test.go`:

```go
package githost

import "testing"

// TestHostInterfaceShape locks the public surface of the Host interface so
// downstream callers (autopilot finalizer, fake) compile against a stable shape.
func TestHostInterfaceShape(t *testing.T) {
	var _ Host = (*CLIHost)(nil)
	var _ Host = (*FakeHost)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githost/... 2>&1`
Expected: FAIL — `undefined: Host`, `undefined: CLIHost`, `undefined: FakeHost`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/githost/githost.go`:

```go
// Package githost wraps the GitHub host (currently the `gh` CLI) for AIOS's
// autopilot finalizer. Two implementations: CLIHost (real, shells out to gh)
// and FakeHost (deterministic in-memory state machine for tests).
package githost

import (
	"context"
	"errors"
	"time"
)

// MergeMode is how a PR should be merged. Only Squash is implemented in M1
// because the spec mandates one tidy commit per autopilot run on main.
type MergeMode string

const (
	MergeSquash MergeMode = "squash"
	MergeMerge  MergeMode = "merge"
	MergeRebase MergeMode = "rebase"
)

// PR identifies a pull request opened against the host.
type PR struct {
	Number int    // PR number, e.g. 42
	URL    string // full URL, e.g. https://github.com/owner/repo/pull/42
	Head   string // head branch, e.g. aios/staging
	Base   string // base branch, e.g. main
}

// ChecksState summarises the aggregate result of all required checks on a PR
// at a single polling moment. It is a value type so callers can compare it.
type ChecksState string

const (
	ChecksPending ChecksState = "pending"
	ChecksGreen   ChecksState = "green"
	ChecksRed     ChecksState = "red"
)

// ErrChecksTimeout is returned by WaitForChecks when the deadline elapses
// before any required check transitions away from pending.
var ErrChecksTimeout = errors.New("checks did not complete before timeout")

// Host is the GitHub adapter the autopilot finalizer talks to. Two impls:
// CLIHost (real `gh`) and FakeHost (test double).
type Host interface {
	// OpenPR opens a PR from head→base with the given title and body, and
	// returns the resulting PR. Idempotency is the caller's responsibility:
	// re-opening when one already exists is the host's behaviour to define.
	OpenPR(ctx context.Context, base, head, title, body string) (*PR, error)

	// WaitForChecks polls all required checks on the PR until the aggregate
	// state is green or red, or until timeout fires. Returns ErrChecksTimeout
	// on deadline. Polling cadence is host-defined (default 10s for CLI).
	WaitForChecks(ctx context.Context, pr *PR, timeout time.Duration) (ChecksState, error)

	// MergePR merges the PR with the requested mode. Returns nil on success.
	// Implementations must refuse to merge when checks are not green; that is
	// a safety invariant of the autopilot — the caller should have verified
	// state with WaitForChecks first, but MergePR is the last line of defence.
	MergePR(ctx context.Context, pr *PR, mode MergeMode) error
}

// CLIHost and FakeHost are declared in cli.go and fake.go respectively.
// Empty placeholder structs here so the interface assertions in tests
// compile before those files are written.
type CLIHost struct{ _ struct{} }
type FakeHost struct{ _ struct{} }

func (*CLIHost) OpenPR(context.Context, string, string, string, string) (*PR, error) {
	return nil, errors.New("not implemented")
}
func (*CLIHost) WaitForChecks(context.Context, *PR, time.Duration) (ChecksState, error) {
	return "", errors.New("not implemented")
}
func (*CLIHost) MergePR(context.Context, *PR, MergeMode) error {
	return errors.New("not implemented")
}

func (*FakeHost) OpenPR(context.Context, string, string, string, string) (*PR, error) {
	return nil, errors.New("not implemented")
}
func (*FakeHost) WaitForChecks(context.Context, *PR, time.Duration) (ChecksState, error) {
	return "", errors.New("not implemented")
}
func (*FakeHost) MergePR(context.Context, *PR, MergeMode) error {
	return errors.New("not implemented")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/githost/... 2>&1`
Expected: PASS.

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/githost/githost.go internal/githost/githost_test.go
git commit -m "feat(githost): define Host interface, PR/MergeMode/ChecksState types"
```

---

## Task 2: Implement `CLIHost.OpenPR`

**Files:**
- Create: `internal/githost/cli.go`
- Create: `internal/githost/cli_test.go`

The real implementation shells out to `gh`. To make it unit-testable without spawning real `gh` processes, we plumb an `exec.Command`-shaped function through the struct so tests can inject a fake.

- [ ] **Step 1: Write the failing test**

Create `internal/githost/cli_test.go`:

```go
package githost

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// fakeExec returns a command builder that produces a process which
// emits stdout/exitcode controlled by the test. Pattern: a tiny helper
// process invoked via os.Args[0] -test.run=TestHelperProcess so we don't
// shell out to anything real. Same approach as os/exec stdlib tests.
func fakeExec(stdout string, exitCode int) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(exec.Command("ls").Path, cs...) // path of self
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT=" + stdout,
			"HELPER_EXIT=" + map[bool]string{true: "0", false: "1"}[exitCode == 0],
		}
		return cmd
	}
}

func TestCLIHost_OpenPR_HappyPath(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec(`{"number":42,"url":"https://github.com/owner/repo/pull/42"}`, 0),
	}
	pr, err := host.OpenPR(context.Background(), "main", "aios/staging", "title", "body")
	if err != nil {
		t.Fatalf("OpenPR returned error: %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("PR.Number = %d, want 42", pr.Number)
	}
	if !strings.Contains(pr.URL, "/pull/42") {
		t.Errorf("PR.URL = %q, want path /pull/42", pr.URL)
	}
	if pr.Head != "aios/staging" {
		t.Errorf("PR.Head = %q, want aios/staging", pr.Head)
	}
	if pr.Base != "main" {
		t.Errorf("PR.Base = %q, want main", pr.Base)
	}
}

func TestCLIHost_OpenPR_GhFailure(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec("", 1),
	}
	_, err := host.OpenPR(context.Background(), "main", "aios/staging", "title", "body")
	if err == nil {
		t.Fatal("OpenPR should fail when gh exits non-zero")
	}
	if !strings.Contains(err.Error(), "gh pr create") {
		t.Errorf("error %q should reference 'gh pr create'", err.Error())
	}
	_ = errors.New // keep import
}
```

Also add this helper at the bottom of `cli_test.go` (must be in the same `_test.go` file so the test binary picks it up):

```go
// TestHelperProcess is the child process spawned by fakeExec. It is invoked
// indirectly when a test runs the fake command. It writes HELPER_STDOUT and
// exits with HELPER_EXIT. Pattern borrowed from os/exec stdlib tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if s := os.Getenv("HELPER_STDOUT"); s != "" {
		_, _ = os.Stdout.WriteString(s)
	}
	if os.Getenv("HELPER_EXIT") != "0" {
		os.Exit(1)
	}
	os.Exit(0)
}
```

Final imports for `cli_test.go`: `context`, `errors`, `os`, `os/exec`, `strings`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githost/... -run TestCLIHost_OpenPR -v`
Expected: FAIL — `host.exec undefined`, `host.OpenPR returns "not implemented"`.

- [ ] **Step 3: Write minimal implementation**

Replace the placeholder `CLIHost` in `internal/githost/githost.go` (delete lines defining the placeholder struct and its methods), then create `internal/githost/cli.go`:

```go
package githost

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// CLIHost implements Host by shelling out to the `gh` CLI. Callers must have
// `gh` on PATH and a valid authenticated session (`gh auth status` clean).
// Both invariants are enforced by the autopilot preflight, not here.
type CLIHost struct {
	// exec is the command builder. Real usage leaves it nil and falls back to
	// exec.Command. Tests inject a fake to avoid spawning real `gh` processes.
	exec func(name string, args ...string) *exec.Cmd
}

// NewCLIHost returns a CLIHost using the real `os/exec` package.
func NewCLIHost() *CLIHost { return &CLIHost{} }

func (h *CLIHost) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if h.exec != nil {
		c := h.exec(name, args...)
		// Tests don't care about ctx; production does.
		return c
	}
	return exec.CommandContext(ctx, name, args...)
}

// ghPRJSON matches the subset of `gh pr create --json number,url` output we use.
type ghPRJSON struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

func (h *CLIHost) OpenPR(ctx context.Context, base, head, title, body string) (*PR, error) {
	cmd := h.cmd(ctx, "gh", "pr", "create",
		"--base", base,
		"--head", head,
		"--title", title,
		"--body", body,
		// gh prints the URL on success; --json gives us a stable parse.
		"--json", "number,url",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w", err)
	}
	var parsed ghPRJSON
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("gh pr create: parse json: %w (raw: %q)", err, string(out))
	}
	return &PR{
		Number: parsed.Number,
		URL:    parsed.URL,
		Head:   head,
		Base:   base,
	}, nil
}

// WaitForChecks and MergePR are implemented in subsequent tasks.
func (h *CLIHost) WaitForChecks(ctx context.Context, pr *PR, timeout time.Duration) (ChecksState, error) {
	return "", fmt.Errorf("WaitForChecks: not implemented")
}
func (h *CLIHost) MergePR(ctx context.Context, pr *PR, mode MergeMode) error {
	return fmt.Errorf("MergePR: not implemented")
}
```

Also delete the placeholder `CLIHost` declarations from `internal/githost/githost.go` (only the `FakeHost` placeholder stays for now; it's removed in Task 4).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/githost/... -run TestCLIHost_OpenPR -v`
Expected: PASS — both `_HappyPath` and `_GhFailure`.

Run: `go test ./internal/githost/... -run TestHostInterfaceShape -v`
Expected: PASS — interface still satisfied.

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/githost/cli.go internal/githost/cli_test.go internal/githost/githost.go
git commit -m "feat(githost): CLIHost.OpenPR via 'gh pr create --json'"
```

---

## Task 3: Implement `CLIHost.WaitForChecks`

**Files:**
- Modify: `internal/githost/cli.go`
- Modify: `internal/githost/cli_test.go`

`gh pr checks <num> --json bucket` returns one row per check with a "bucket" of `pass`/`fail`/`pending`/`skipping`/`cancel`. Aggregate rule:
- Any `fail` or `cancel` → `ChecksRed`.
- All `pass` (and at least one row) → `ChecksGreen`.
- Otherwise → `ChecksPending` (keep polling).

- [ ] **Step 1: Write the failing tests**

Add to `internal/githost/cli_test.go`:

```go
import "time"  // add to existing imports

func TestCLIHost_WaitForChecks_GreenOnFirstPoll(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec(`[{"bucket":"pass"},{"bucket":"pass"}]`, 0),
	}
	state, err := host.WaitForChecks(context.Background(), &PR{Number: 1}, 1*time.Second)
	if err != nil {
		t.Fatalf("WaitForChecks: %v", err)
	}
	if state != ChecksGreen {
		t.Errorf("state = %q, want %q", state, ChecksGreen)
	}
}

func TestCLIHost_WaitForChecks_RedShortCircuits(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec(`[{"bucket":"pass"},{"bucket":"fail"}]`, 0),
	}
	state, err := host.WaitForChecks(context.Background(), &PR{Number: 1}, 1*time.Second)
	if err != nil {
		t.Fatalf("WaitForChecks: %v", err)
	}
	if state != ChecksRed {
		t.Errorf("state = %q, want %q", state, ChecksRed)
	}
}

func TestCLIHost_WaitForChecks_TimeoutWhenAllPending(t *testing.T) {
	host := &CLIHost{
		exec:     fakeExec(`[{"bucket":"pending"}]`, 0),
		pollEvery: 10 * time.Millisecond,
	}
	_, err := host.WaitForChecks(context.Background(), &PR{Number: 1}, 30*time.Millisecond)
	if !errors.Is(err, ErrChecksTimeout) {
		t.Errorf("err = %v, want ErrChecksTimeout", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/githost/... -run TestCLIHost_WaitForChecks -v`
Expected: FAIL — three failures, all citing `WaitForChecks: not implemented`.

- [ ] **Step 3: Write minimal implementation**

Replace the stub `WaitForChecks` in `internal/githost/cli.go`:

```go
// ghCheckRow matches the subset of `gh pr checks --json bucket` output we use.
// `bucket` is `pass | fail | pending | skipping | cancel`.
type ghCheckRow struct {
	Bucket string `json:"bucket"`
}

// pollEvery is the interval between `gh pr checks` polls. Exposed as a struct
// field (rather than a const) so tests can drive the timeout path quickly.
// Default 10s if zero.
func (h *CLIHost) pollInterval() time.Duration {
	if h.pollEvery > 0 {
		return h.pollEvery
	}
	return 10 * time.Second
}

func (h *CLIHost) WaitForChecks(ctx context.Context, pr *PR, timeout time.Duration) (ChecksState, error) {
	deadline := time.Now().Add(timeout)
	for {
		cmd := h.cmd(ctx, "gh", "pr", "checks", fmt.Sprintf("%d", pr.Number), "--json", "bucket")
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("gh pr checks: %w", err)
		}
		var rows []ghCheckRow
		if err := json.Unmarshal(out, &rows); err != nil {
			return "", fmt.Errorf("gh pr checks: parse json: %w (raw: %q)", err, string(out))
		}
		state := aggregateChecks(rows)
		if state != ChecksPending {
			return state, nil
		}
		if time.Now().After(deadline) {
			return "", ErrChecksTimeout
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(h.pollInterval()):
		}
	}
}

// aggregateChecks applies the precedence rule: any fail/cancel → red; all
// pass and ≥1 row → green; otherwise pending (keep polling). Empty input is
// pending — gh sometimes returns [] for the brief window between PR open and
// the first check kicking off.
func aggregateChecks(rows []ghCheckRow) ChecksState {
	if len(rows) == 0 {
		return ChecksPending
	}
	allPass := true
	for _, r := range rows {
		switch r.Bucket {
		case "fail", "cancel":
			return ChecksRed
		case "pass", "skipping":
			// counted as green-equivalent
		default:
			allPass = false
		}
	}
	if allPass {
		return ChecksGreen
	}
	return ChecksPending
}
```

Also add `pollEvery time.Duration` field to `CLIHost`:

```go
type CLIHost struct {
	exec      func(name string, args ...string) *exec.Cmd
	pollEvery time.Duration // 0 = use default (10s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/githost/... -run TestCLIHost_WaitForChecks -v`
Expected: PASS — all three.

Run: `go test ./internal/githost/...`
Expected: PASS overall.

- [ ] **Step 5: Commit**

```bash
git add internal/githost/cli.go internal/githost/cli_test.go
git commit -m "feat(githost): CLIHost.WaitForChecks with bucket-based aggregation"
```

---

## Task 4: Implement `CLIHost.MergePR` and `FakeHost`

**Files:**
- Modify: `internal/githost/cli.go`
- Modify: `internal/githost/cli_test.go`
- Create: `internal/githost/fake.go`
- Create: `internal/githost/fake_test.go`
- Modify: `internal/githost/githost.go` (delete `FakeHost` placeholder)

- [ ] **Step 1: Write the failing tests for MergePR**

Add to `internal/githost/cli_test.go`:

```go
func TestCLIHost_MergePR_SquashCallsCorrectFlags(t *testing.T) {
	var captured []string
	host := &CLIHost{
		exec: func(name string, args ...string) *exec.Cmd {
			captured = append([]string{name}, args...)
			return fakeExec("", 0)(name, args...)
		},
	}
	err := host.MergePR(context.Background(), &PR{Number: 7}, MergeSquash)
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
	want := []string{"gh", "pr", "merge", "7", "--squash", "--delete-branch"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_MergePR_GhFailureSurfaces(t *testing.T) {
	host := &CLIHost{exec: fakeExec("", 1)}
	err := host.MergePR(context.Background(), &PR{Number: 7}, MergeSquash)
	if err == nil {
		t.Fatal("MergePR should fail when gh exits non-zero")
	}
}
```

Add `"reflect"` to the test file's imports.

- [ ] **Step 2: Write the failing tests for FakeHost**

Create `internal/githost/fake_test.go`:

```go
package githost

import (
	"context"
	"testing"
	"time"
)

func TestFakeHost_OpenPRReturnsIncrementingNumbers(t *testing.T) {
	f := &FakeHost{}
	pr1, err := f.OpenPR(context.Background(), "main", "feat/a", "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	pr2, err := f.OpenPR(context.Background(), "main", "feat/b", "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if pr1.Number == pr2.Number {
		t.Errorf("PR numbers collided: %d == %d", pr1.Number, pr2.Number)
	}
}

func TestFakeHost_WaitForChecksReturnsConfiguredState(t *testing.T) {
	f := &FakeHost{ChecksByPR: map[int]ChecksState{1: ChecksGreen}}
	pr, _ := f.OpenPR(context.Background(), "main", "h", "t", "b")
	if pr.Number != 1 {
		t.Fatalf("expected PR #1, got %d", pr.Number)
	}
	state, err := f.WaitForChecks(context.Background(), pr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state != ChecksGreen {
		t.Errorf("state = %q, want green", state)
	}
}

func TestFakeHost_MergePRRefusesIfChecksNotGreen(t *testing.T) {
	f := &FakeHost{ChecksByPR: map[int]ChecksState{1: ChecksRed}}
	pr, _ := f.OpenPR(context.Background(), "main", "h", "t", "b")
	err := f.MergePR(context.Background(), pr, MergeSquash)
	if err == nil {
		t.Error("MergePR should refuse a red PR")
	}
}

func TestFakeHost_MergePRMarksMerged(t *testing.T) {
	f := &FakeHost{ChecksByPR: map[int]ChecksState{1: ChecksGreen}}
	pr, _ := f.OpenPR(context.Background(), "main", "h", "t", "b")
	if err := f.MergePR(context.Background(), pr, MergeSquash); err != nil {
		t.Fatal(err)
	}
	if !f.Merged[pr.Number] {
		t.Errorf("PR %d should be marked merged", pr.Number)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/githost/... -run "TestCLIHost_MergePR|TestFakeHost" -v`
Expected: FAIL — `MergePR: not implemented`, `FakeHost.OpenPR: not implemented`.

- [ ] **Step 4: Write minimal implementation**

Replace the stub `MergePR` in `internal/githost/cli.go`:

```go
func (h *CLIHost) MergePR(ctx context.Context, pr *PR, mode MergeMode) error {
	flag := ""
	switch mode {
	case MergeSquash:
		flag = "--squash"
	case MergeMerge:
		flag = "--merge"
	case MergeRebase:
		flag = "--rebase"
	default:
		return fmt.Errorf("unknown merge mode %q", mode)
	}
	cmd := h.cmd(ctx, "gh", "pr", "merge", fmt.Sprintf("%d", pr.Number), flag, "--delete-branch")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr merge: %w (output: %q)", err, string(out))
	}
	return nil
}
```

Delete the `FakeHost` placeholder block from `internal/githost/githost.go` (the empty `FakeHost struct{ _ struct{} }` and its three `not implemented` methods).

Create `internal/githost/fake.go`:

```go
package githost

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// FakeHost is an in-memory Host for tests. It is goroutine-safe so concurrent
// integration tests don't race on the merged map.
type FakeHost struct {
	mu     sync.Mutex
	nextID int
	prs    map[int]*PR

	// ChecksByPR lets tests configure WaitForChecks return values. Missing
	// entries are treated as ChecksGreen for ergonomics.
	ChecksByPR map[int]ChecksState
	// Merged records which PRs MergePR was called on. Read by tests.
	Merged map[int]bool
	// OpenedPRs records every PR opened, in order, so tests can assert on
	// title/body/branches without poking internal state.
	OpenedPRs []*PR
}

func (f *FakeHost) OpenPR(_ context.Context, base, head, title, body string) (*PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prs == nil {
		f.prs = map[int]*PR{}
	}
	if f.Merged == nil {
		f.Merged = map[int]bool{}
	}
	f.nextID++
	pr := &PR{
		Number: f.nextID,
		URL:    fmt.Sprintf("https://example.invalid/pull/%d", f.nextID),
		Head:   head,
		Base:   base,
	}
	f.prs[pr.Number] = pr
	f.OpenedPRs = append(f.OpenedPRs, pr)
	return pr, nil
}

func (f *FakeHost) WaitForChecks(_ context.Context, pr *PR, _ time.Duration) (ChecksState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state, ok := f.ChecksByPR[pr.Number]; ok {
		return state, nil
	}
	return ChecksGreen, nil
}

func (f *FakeHost) MergePR(_ context.Context, pr *PR, _ MergeMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state, ok := f.ChecksByPR[pr.Number]; ok && state != ChecksGreen {
		return errors.New("FakeHost: refusing to merge a non-green PR")
	}
	if f.Merged == nil {
		f.Merged = map[int]bool{}
	}
	f.Merged[pr.Number] = true
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/githost/... -v`
Expected: PASS — all CLIHost and FakeHost tests, including the existing `TestHostInterfaceShape`.

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/githost/
git commit -m "feat(githost): CLIHost.MergePR (squash+delete-branch) and FakeHost test double"
```

---

## Task 5: Add `--auto` flag to `aios new`

**Files:**
- Modify: `internal/cli/new.go`
- Create: `internal/cli/new_test.go`

The change: extract the body of `newNewCmd()` into a function `runNew(opts NewOpts) error`, gate the `confirm()` call on `!opts.Auto`. Backward compatibility is preserved because the existing interactive command still calls `runNew` with `Auto: false`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/new_test.go`:

```go
package cli

import "testing"

// TestNewOptsAutoSkipsConfirm is a compile-time assertion that NewOpts has
// the Auto field. The behaviour test that --auto actually skips the prompt
// is covered end-to-end by test/integration/autopilot_oneshot_test.go,
// because runNew talks to real engines and a real git repo, which a unit
// test cannot economically stub.
func TestNewOptsHasAutoField(t *testing.T) {
	var o NewOpts
	o.Auto = true
	if !o.Auto {
		t.Error("NewOpts.Auto roundtrip failed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestNewOptsHasAutoField -v`
Expected: FAIL — `undefined: NewOpts`.

- [ ] **Step 3: Write minimal implementation**

Refactor `internal/cli/new.go`. Replace the entire file with:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/spf13/cobra"
)

// NewOpts is the struct form of `aios new` arguments. Extracted so the
// autopilot command can call runNew without going through Cobra flag plumbing.
type NewOpts struct {
	Idea string
	Auto bool // skip the "Confirm and commit?" prompt
}

func newNewCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "new <idea>",
		Short: "Brainstorm an idea into a spec + task list",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			auto, _ := cmd.Flags().GetBool("auto")
			return runNew(NewOpts{Idea: strings.Join(args, " "), Auto: auto})
		},
	}
	c.Flags().Bool("auto", false, "skip the spec/tasks confirmation prompt and commit unconditionally")
	return c
}

func runNew(opts NewOpts) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("run `aios init` first: %w", err)
	}

	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}

	claude := &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec}
	codex := &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec}

	bPrompt, _ := prompts.Render("brainstorm.tmpl", map[string]string{"Idea": opts.Idea})
	bRes, err := claude.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: bPrompt})
	if err != nil {
		return err
	}
	_ = rec.WriteFile("brainstorm.md", []byte(bRes.Text))

	sPrompt, _ := prompts.Render("spec-synth.tmpl", map[string]string{"Transcript": bRes.Text})
	sRes, err := claude.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: sPrompt})
	if err != nil {
		return err
	}
	specPath := filepath.Join(wd, ".aios", "project.md")
	_ = os.MkdirAll(filepath.Dir(specPath), 0o755)
	if err := os.WriteFile(specPath, []byte(sRes.Text), 0o644); err != nil {
		return err
	}

	dPrompt, _ := prompts.Render("decompose.tmpl", map[string]string{"Spec": sRes.Text})
	dRes, err := codex.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return err
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	_ = os.MkdirAll(tasksDir, 0o755)
	written, err := writeTaskFiles(tasksDir, dRes.Text)
	if err != nil {
		return err
	}

	fmt.Printf("\nSpec written to %s\n", specPath)
	fmt.Printf("Task files written to %s (%d files)\n\n", tasksDir, written)

	// Auto mode skips the confirmation entirely. Used by `aios autopilot`
	// and by the M4 issue-bot. Legacy interactive `aios new` retains the gate.
	if !opts.Auto {
		if !confirm("Confirm and commit to aios/staging? [y/N] ") {
			fmt.Println("Left spec + tasks uncommitted. Edit and re-run `aios new --resume` to retry.")
			return nil
		}
	}

	if err := commitNewSpec(wd, cfg.Project.StagingBranch, opts.Idea); err != nil {
		return err
	}
	fmt.Println("Committed to " + cfg.Project.StagingBranch)
	return nil
}

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

func extractTaskID(frontmatter string) string {
	for _, ln := range strings.Split(frontmatter, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "id:"))
		}
	}
	return ""
}

func commitNewSpec(wd, staging, idea string) error {
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
	msg := "aios: spec and tasks for " + idea
	return exec.Command("git", "-C", wd, "commit", "-m", msg).Run()
}

// keep bufio import used somewhere
var _ = bufio.NewReader
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run TestNewOptsHasAutoField -v`
Expected: PASS.

Run: `go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/new.go internal/cli/new_test.go
git commit -m "feat(cli): aios new --auto skips confirmation prompt"
```

---

## Task 6: Implement abandoned-task artifact writer

**Files:**
- Create: `internal/run/abandoned.go`
- Create: `internal/run/abandoned_test.go`

When autopilot mode encounters a stuck task, the orchestrator's `*Outcome` is captured to disk so the user can audit it later. Layout: `.aios/runs/<run>/abandoned/<task>/{report.md,full-trail.json}`.

- [ ] **Step 1: Write the failing test**

Create `internal/run/abandoned_test.go`:

```go
package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAbandoned_WritesReportAndJSON(t *testing.T) {
	dir := t.TempDir()
	rec, err := Open(dir, "run-id")
	if err != nil {
		t.Fatal(err)
	}
	rounds := []AbandonedRound{
		{N: 1, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
		{N: 2, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
		{N: 3, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
	}
	info := AbandonedInfo{
		TaskID:      "004",
		Reason:      "stall_no_progress: 3 consecutive rounds raised identical review issues",
		BlockCode:   "stall_no_progress",
		UsageTokens: 12_345,
		Rounds:      rounds,
	}
	if err := WriteAbandoned(rec, info); err != nil {
		t.Fatalf("WriteAbandoned: %v", err)
	}

	reportPath := filepath.Join(rec.Root(), "abandoned", "004", "report.md")
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	if !strings.Contains(string(body), "004") {
		t.Errorf("report.md missing task ID: %q", body)
	}
	if !strings.Contains(string(body), "stall_no_progress") {
		t.Errorf("report.md missing block code: %q", body)
	}

	jsonPath := filepath.Join(rec.Root(), "abandoned", "004", "full-trail.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read full-trail.json: %v", err)
	}
	var roundtrip AbandonedInfo
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatalf("full-trail.json is not valid JSON: %v", err)
	}
	if roundtrip.TaskID != "004" || roundtrip.UsageTokens != 12_345 {
		t.Errorf("roundtrip mismatch: %+v", roundtrip)
	}
}

func TestWriteAbandoned_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rec, _ := Open(dir, "run-id")
	info := AbandonedInfo{TaskID: "004", BlockCode: "x"}
	if err := WriteAbandoned(rec, info); err != nil {
		t.Fatal(err)
	}
	if err := WriteAbandoned(rec, info); err != nil {
		t.Errorf("second WriteAbandoned should overwrite cleanly, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/run/... -run TestWriteAbandoned -v`
Expected: FAIL — `undefined: AbandonedInfo`, `undefined: WriteAbandoned`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/run/abandoned.go`:

```go
package run

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// AbandonedRound is the per-round summary captured for an abandoned task.
// Smaller than orchestrator.RoundRecord — full prompts/responses are already
// persisted under round-N/, so this is a flat index.
type AbandonedRound struct {
	N              int  `json:"n"`
	ReviewApproved bool `json:"review_approved"`
	IssueCount     int  `json:"issue_count"`
	VerifyGreen    bool `json:"verify_green"`
	Escalated      bool `json:"escalated,omitempty"`
}

// AbandonedInfo is the audit-trail summary for a task that was abandoned in
// autopilot mode. Round-level prompts/responses live alongside under
// round-N/; this struct is the index a future reader hits first.
type AbandonedInfo struct {
	TaskID      string           `json:"task_id"`
	Reason      string           `json:"reason"`     // BlockReason.String()
	BlockCode   string           `json:"block_code"` // orchestrator.BlockCode value
	UsageTokens int              `json:"usage_tokens"`
	Rounds      []AbandonedRound `json:"rounds"`
}

// WriteAbandoned persists report.md + full-trail.json under
// .aios/runs/<id>/abandoned/<task>/. Overwrites are intentional — re-running
// against the same task ID should refresh the artefact, not error.
func WriteAbandoned(rec *Recorder, info AbandonedInfo) error {
	if info.TaskID == "" {
		return fmt.Errorf("WriteAbandoned: empty TaskID")
	}
	rel := filepath.Join("abandoned", info.TaskID)
	if err := rec.WriteFile(filepath.Join(rel, "report.md"), []byte(renderAbandonedReport(info))); err != nil {
		return fmt.Errorf("write report.md: %w", err)
	}
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal full-trail: %w", err)
	}
	if err := rec.WriteFile(filepath.Join(rel, "full-trail.json"), raw); err != nil {
		return fmt.Errorf("write full-trail.json: %w", err)
	}
	return nil
}

func renderAbandonedReport(info AbandonedInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Abandoned: %s\n\n", info.TaskID)
	fmt.Fprintf(&b, "**Block code:** `%s`\n\n", info.BlockCode)
	if info.Reason != "" {
		fmt.Fprintf(&b, "**Reason:** %s\n\n", info.Reason)
	}
	fmt.Fprintf(&b, "**Tokens used:** %d\n\n", info.UsageTokens)
	if len(info.Rounds) > 0 {
		b.WriteString("## Rounds\n\n")
		b.WriteString("| Round | Approved | Issues | Verify | Escalated |\n")
		b.WriteString("|---:|:---:|---:|:---:|:---:|\n")
		for _, r := range info.Rounds {
			fmt.Fprintf(&b, "| %d | %v | %d | %v | %v |\n",
				r.N, r.ReviewApproved, r.IssueCount, r.VerifyGreen, r.Escalated)
		}
	}
	b.WriteString("\nFull per-round prompts and responses live alongside this file under `round-N/`.\n")
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/run/... -run TestWriteAbandoned -v`
Expected: PASS — both subtests.

Run: `go test ./internal/run/...`
Expected: PASS overall.

- [ ] **Step 5: Commit**

```bash
git add internal/run/abandoned.go internal/run/abandoned_test.go
git commit -m "feat(run): WriteAbandoned persists abandoned-task audit trail"
```

---

## Task 7: Add autopilot preflight (gh on PATH, gh auth status, remote exists)

**Files:**
- Create: `internal/cli/preflight_autopilot.go`
- Create: `internal/cli/preflight_autopilot_test.go`

This preflight runs **before any model invocation** in autopilot mode. It is separate from the existing `preflight()` (working-tree clean, base branch ancestry, engine binaries) so it can be unit-tested without needing a git fixture.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/preflight_autopilot_test.go`:

```go
package cli

import (
	"errors"
	"os/exec"
	"testing"
)

func TestAutopilotPreflight_GhMissing(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		runCmd:   func(*exec.Cmd) error { return nil },
		hasRemote: func() (bool, error) { return true, nil },
	}
	err := pre.Check()
	if err == nil || !contains(err.Error(), "gh") {
		t.Errorf("err = %v, want one mentioning 'gh'", err)
	}
}

func TestAutopilotPreflight_GhAuthBroken(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/gh", nil },
		runCmd:    func(*exec.Cmd) error { return errors.New("auth: not logged in") },
		hasRemote: func() (bool, error) { return true, nil },
	}
	err := pre.Check()
	if err == nil || !contains(err.Error(), "gh auth") {
		t.Errorf("err = %v, want one mentioning 'gh auth'", err)
	}
}

func TestAutopilotPreflight_NoRemote(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/gh", nil },
		runCmd:    func(*exec.Cmd) error { return nil },
		hasRemote: func() (bool, error) { return false, nil },
	}
	err := pre.Check()
	if err == nil || !contains(err.Error(), "remote") {
		t.Errorf("err = %v, want one mentioning 'remote'", err)
	}
}

func TestAutopilotPreflight_HappyPath(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/gh", nil },
		runCmd:    func(*exec.Cmd) error { return nil },
		hasRemote: func() (bool, error) { return true, nil },
	}
	if err := pre.Check(); err != nil {
		t.Errorf("happy path returned err: %v", err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || index(s, sub) >= 0) }
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestAutopilotPreflight -v`
Expected: FAIL — `undefined: autopilotPreflight`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/preflight_autopilot.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestAutopilotPreflight -v`
Expected: PASS — all four subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/preflight_autopilot.go internal/cli/preflight_autopilot_test.go
git commit -m "feat(cli): autopilotPreflight checks gh, gh auth, git remote"
```

---

## Task 8: Add `aios run --autopilot` flag (abandoned-task drop path)

**Files:**
- Modify: `internal/cli/run.go`

The change: a new `--autopilot` boolean flag. When true, a `StateBlocked` outcome with `CodeStallNoProgress` (the `[NEEDS HUMAN]` case) is converted to "write abandoned artifact, mark task `abandoned`, continue rest of run" instead of "block and stop." All other block codes still terminate the task — autopilot only rescues stall blocks, not engine errors or budget exhaustion.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/run.go` testing — but since the existing `runMain` is end-to-end and not unit-tested directly, the assertion in this task is integration-level. Mark this as a behavioural step that the integration test in Task 11 will cover. For this task, the verification is **build + vet clean + the existing test suite stays green**.

We'll still write a tiny unit test for the helper that decides whether autopilot should rescue a block. Add to `internal/cli/run_autopilot_test.go` (new file):

```go
package cli

import (
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
)

func TestAutopilotRescues_OnlyStall(t *testing.T) {
	cases := []struct {
		code orchestrator.BlockCode
		want bool
	}{
		{orchestrator.CodeStallNoProgress, true},
		{orchestrator.CodeMaxRoundsExceeded, false},
		{orchestrator.CodeMaxTokensExceeded, false},
		{orchestrator.CodeEngineInvokeFailed, false},
		{orchestrator.CodeRebaseConflict, false},
		{orchestrator.CodeUpstreamBlocked, false},
	}
	for _, c := range cases {
		got := autopilotRescues(c.code)
		if got != c.want {
			t.Errorf("autopilotRescues(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestAutopilotRescues_OnlyStall -v`
Expected: FAIL — `undefined: autopilotRescues`.

- [ ] **Step 3: Write minimal implementation — add the flag and helper**

Edit `internal/cli/run.go`:

(a) Add the flag in `newRunCmd()`. Find:

```go
	c.Flags().Int("max-tokens-run", 0, "override [parallel] max_tokens_per_run (0 = use config)")
	return c
```

Replace with:

```go
	c.Flags().Int("max-tokens-run", 0, "override [parallel] max_tokens_per_run (0 = use config)")
	c.Flags().Bool("autopilot", false, "drop stalled tasks instead of blocking with [NEEDS HUMAN]")
	c.Flags().Bool("merge", false, "after a successful run, open PR aios/staging→main, wait for CI, squash-merge")
	return c
```

(b) Add the helper at the bottom of `internal/cli/run.go`:

```go
// autopilotRescues returns true when a block code represents a "stall" that
// autopilot mode should convert into an abandoned-task drop. All other codes
// (budget exhaustion, engine errors, git failures, upstream blocks) are real
// errors that should terminate the task even in autopilot — they are not
// "the model couldn't make the reviewer happy" cases.
func autopilotRescues(code orchestrator.BlockCode) bool {
	return code == orchestrator.CodeStallNoProgress
}
```

(c) In `runMain`, read the flag at the top alongside the existing flags:

Find this block (around line 60):

```go
	if mtr, _ := cmd.Flags().GetInt("max-tokens-run"); mtr > 0 {
		runTokenCap = mtr
	}
```

Add immediately after it:

```go
	autopilot, _ := cmd.Flags().GetBool("autopilot")
	mergeAfter, _ := cmd.Flags().GetBool("merge")
	_ = mergeAfter // wired in Task 9
```

(d) Find the abandon site in `taskFn`. Around line 345:

```go
		if outcome.Final != orchestrator.StateConverged {
			tk.Status = "blocked"
			rpt := buildReport(tk, outcome)
			_ = rec.WriteTaskFile(tk.ID, "report.md", []byte(run.RenderReport(rpt)))
			_ = updateTaskFile(tk)
			fmt.Printf("✗ task %s BLOCKED: %s\n", tk.ID, outcome.Reason)
			return orchestrator.TaskResult{
				ID:          id,
				Status:      "blocked",
				Reason:      outcome.Reason,
				BlockReason: outcome.BlockReason,
			}, nil
		}
```

Replace with:

```go
		if outcome.Final != orchestrator.StateConverged {
			// Autopilot mode rescues stall blocks: write the audit trail to
			// .aios/runs/<id>/abandoned/<task>/, mark the task abandoned, and
			// let the rest of the run proceed. Non-stall blocks (budget,
			// engine errors, git failures) still terminate this task.
			if autopilot && outcome.BlockReason != nil && autopilotRescues(outcome.BlockReason.Code) {
				info := run.AbandonedInfo{
					TaskID:      tk.ID,
					Reason:      outcome.Reason,
					BlockCode:   string(outcome.BlockReason.Code),
					UsageTokens: outcome.UsageTokens,
					Rounds:      summarizeRoundsForAbandon(outcome.Rounds),
				}
				if werr := run.WriteAbandoned(rec, info); werr != nil {
					fmt.Fprintf(os.Stderr, "warn: write abandoned artifact for %s: %v\n", tk.ID, werr)
				}
				tk.Status = "abandoned"
				_ = updateTaskFile(tk)
				fmt.Printf("⚠ task %s ABANDONED (autopilot): %s\n", tk.ID, outcome.Reason)
				return orchestrator.TaskResult{
					ID:          id,
					Status:      "abandoned",
					Reason:      outcome.Reason,
					BlockReason: outcome.BlockReason,
				}, nil
			}
			tk.Status = "blocked"
			rpt := buildReport(tk, outcome)
			_ = rec.WriteTaskFile(tk.ID, "report.md", []byte(run.RenderReport(rpt)))
			_ = updateTaskFile(tk)
			fmt.Printf("✗ task %s BLOCKED: %s\n", tk.ID, outcome.Reason)
			return orchestrator.TaskResult{
				ID:          id,
				Status:      "blocked",
				Reason:      outcome.Reason,
				BlockReason: outcome.BlockReason,
			}, nil
		}
```

(e) Add the helper at the bottom of `run.go`:

```go
// summarizeRoundsForAbandon converts orchestrator round records to the
// flatter form WriteAbandoned expects. Full prompts and responses are
// already persisted under round-N/; this is just an index.
func summarizeRoundsForAbandon(rs []orchestrator.RoundRecord) []run.AbandonedRound {
	out := make([]run.AbandonedRound, 0, len(rs))
	for _, r := range rs {
		out = append(out, run.AbandonedRound{
			N:              r.N,
			ReviewApproved: r.Review.Approved,
			IssueCount:     len(r.Review.Issues),
			VerifyGreen:    allChecksGreen(r.Checks),
			Escalated:      r.Escalated,
		})
	}
	return out
}

func allChecksGreen(cs []verify.CheckResult) bool {
	return verify.AllGreen(cs)
}
```

`verify` is already imported at the top of run.go.

(f) Update the block-summary printer to skip abandoned tasks. Find `printBlockSummary`:

```go
func printBlockSummary(rep *orchestrator.RunReport) {
	fmt.Fprintf(os.Stderr, "\n%d task(s) blocked:\n", len(rep.Blocked))
```

Add a line just before the `fmt.Fprintf` to count only entries that are real blocks (not abandons). The orchestrator's `RunReport.Blocked` already excludes abandoned tasks because they returned `Status: "abandoned"` not `"blocked"` — verify by reading the integration test output in Task 11. No change needed here unless the integration test reveals a leak.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestAutopilotRescues_OnlyStall -v`
Expected: PASS.

Run: `go build ./...`
Expected: clean.

Run: `go test ./...`
Expected: PASS — full suite, including existing integration tests, stays green. (This change is opt-in via `--autopilot`; no existing path is altered.)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/run.go internal/cli/run_autopilot_test.go
git commit -m "feat(cli): aios run --autopilot drops stalled tasks instead of blocking"
```

---

## Task 9: Add `--merge` flag and the autopilot finalizer

**Files:**
- Modify: `internal/cli/run.go`

The finalizer runs only when **both** `--autopilot` and `--merge` are set, after `RunAll` returns. It opens a PR `aios/staging → main`, waits for CI, and squash-merges on green. Failures leave the PR open and surface to the user via exit code + summary.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/run_autopilot_test.go`:

```go
import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestAutopilotFinalizer_NoConvergedTasksSkipsPR(t *testing.T) {
	host := &githost.FakeHost{}
	res, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 0,
		Title:          "t", Body: "b",
		ChecksTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	if res.PR != nil {
		t.Errorf("expected no PR opened when nothing converged, got %+v", res.PR)
	}
}

func TestAutopilotFinalizer_GreenChecksMerge(t *testing.T) {
	host := &githost.FakeHost{
		ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksGreen},
	}
	res, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 1,
		Title:          "t", Body: "b",
		ChecksTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	if res.PR == nil || !host.Merged[res.PR.Number] {
		t.Errorf("expected PR merged on green checks, got %+v", res)
	}
}

func TestAutopilotFinalizer_RedChecksDoesNotMerge(t *testing.T) {
	host := &githost.FakeHost{
		ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksRed},
	}
	res, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 1,
		Title:          "t", Body: "b",
		ChecksTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected error on red checks")
	}
	if res.PR == nil {
		t.Error("PR should still be reported even on red — user needs the URL")
	}
	if host.Merged[res.PR.Number] {
		t.Error("must not merge a red PR")
	}
}

func TestAutopilotFinalizer_TimeoutDoesNotMerge(t *testing.T) {
	// FakeHost.WaitForChecks returns ChecksByPR; if we want a timeout we need
	// a stub that wraps the fake. Inline an adapter.
	host := &timeoutHost{}
	_, err := runAutopilotFinalizer(context.Background(), finalizerOpts{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		ConvergedCount: 1,
		Title:          "t", Body: "b",
		ChecksTimeout: 10 * time.Millisecond,
	})
	if !errors.Is(err, githost.ErrChecksTimeout) {
		t.Errorf("err = %v, want ErrChecksTimeout", err)
	}
	if host.merged {
		t.Error("must not merge on timeout")
	}
}

// timeoutHost is a Host that always returns ErrChecksTimeout from WaitForChecks.
type timeoutHost struct {
	merged bool
}

func (*timeoutHost) OpenPR(_ context.Context, base, head, _, _ string) (*githost.PR, error) {
	return &githost.PR{Number: 1, URL: "url", Head: head, Base: base}, nil
}
func (*timeoutHost) WaitForChecks(context.Context, *githost.PR, time.Duration) (githost.ChecksState, error) {
	return "", githost.ErrChecksTimeout
}
func (h *timeoutHost) MergePR(context.Context, *githost.PR, githost.MergeMode) error {
	h.merged = true
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestAutopilotFinalizer -v`
Expected: FAIL — `undefined: runAutopilotFinalizer`, `undefined: finalizerOpts`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/cli/run.go`:

```go
import (
	// ... existing imports ...
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

// finalizerOpts is the input to runAutopilotFinalizer. Decoupled from runMain
// so the finalizer is unit-testable without spinning up an orchestrator run.
type finalizerOpts struct {
	Host           githost.Host
	Base           string
	Head           string
	Title          string
	Body           string
	ConvergedCount int           // skip everything when 0
	ChecksTimeout  time.Duration // 30 min if zero
}

// finalizerResult reports what the finalizer did. PR is non-nil whenever a
// PR was opened (even if it was not merged, e.g. CI red).
type finalizerResult struct {
	PR        *githost.PR
	State     githost.ChecksState // pending|green|red — green implies Merged
	Merged    bool
	SkipReason string // populated when PR is nil (e.g. "no converged tasks")
}

func runAutopilotFinalizer(ctx context.Context, opts finalizerOpts) (*finalizerResult, error) {
	if opts.ConvergedCount == 0 {
		return &finalizerResult{SkipReason: "no converged tasks"}, nil
	}
	if opts.ChecksTimeout == 0 {
		opts.ChecksTimeout = 30 * time.Minute
	}
	pr, err := opts.Host.OpenPR(ctx, opts.Base, opts.Head, opts.Title, opts.Body)
	if err != nil {
		return &finalizerResult{}, fmt.Errorf("open PR %s→%s: %w", opts.Head, opts.Base, err)
	}
	res := &finalizerResult{PR: pr}
	state, err := opts.Host.WaitForChecks(ctx, pr, opts.ChecksTimeout)
	if err != nil {
		return res, fmt.Errorf("wait for PR #%d checks: %w", pr.Number, err)
	}
	res.State = state
	if state != githost.ChecksGreen {
		return res, fmt.Errorf("PR #%d checks ended %s; not merging — see %s", pr.Number, state, pr.URL)
	}
	if err := opts.Host.MergePR(ctx, pr, githost.MergeSquash); err != nil {
		return res, fmt.Errorf("merge PR #%d: %w", pr.Number, err)
	}
	res.Merged = true
	return res, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestAutopilotFinalizer -v`
Expected: PASS — all four subtests.

Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/run.go internal/cli/run_autopilot_test.go
git commit -m "feat(cli): autopilot finalizer opens PR, polls CI, squash-merges on green"
```

---

## Task 10: Wire the finalizer into `runMain` and add preflight call

**Files:**
- Modify: `internal/cli/run.go`

- [ ] **Step 1: Add the failing test**

The finalizer is unit-tested. The wiring is end-to-end. We'll write the integration test in Task 11. For this task, the verification is: `go build ./...` clean, existing tests still green, and a manual smoke check that the new code paths are reachable.

- [ ] **Step 2: Modify `runMain` to call preflight in autopilot mode**

In `internal/cli/run.go`, find the existing preflight call near the top of `runMain`:

```go
	if err := preflight(wd, cfg); err != nil {
		return err
	}
```

Insert immediately after it:

```go
	// Autopilot adds a second layer of preflight: gh on PATH, gh auth status
	// clean, repo has a remote. Cheap; runs before any model invocation.
	if autopilot {
		if err := newAutopilotPreflight(wd).Check(); err != nil {
			return err
		}
	}
```

- [ ] **Step 3: Modify the tail of `runMain` to call the finalizer**

Find the existing tail:

```go
	if len(rep.Blocked) > 0 {
		printBlockSummary(rep)
		os.Exit(2)
	}
	return nil
}
```

Replace with:

```go
	if len(rep.Blocked) > 0 {
		printBlockSummary(rep)
		os.Exit(2)
	}

	if autopilot && mergeAfter {
		host := githost.NewCLIHost()
		fres, err := runAutopilotFinalizer(runCtx, finalizerOpts{
			Host:           host,
			Base:           cfg.Project.BaseBranch,
			Head:           cfg.Project.StagingBranch,
			Title:          autopilotPRTitle(rep),
			Body:           autopilotPRBody(rep, rec.Root()),
			ConvergedCount: len(rep.Converged),
			ChecksTimeout:  30 * time.Minute,
		})
		// Always write the summary, regardless of finalizer outcome.
		_ = writeAutopilotSummary(rec, fres, err)
		if err != nil {
			fmt.Fprintf(os.Stderr, "autopilot finalizer: %v\n", err)
			os.Exit(2)
		}
		if fres.SkipReason != "" {
			fmt.Printf("autopilot: %s; nothing to merge\n", fres.SkipReason)
			return nil
		}
		fmt.Printf("autopilot: merged PR #%d (%s)\n", fres.PR.Number, fres.PR.URL)
	}
	return nil
}
```

Add the title/body/summary helpers at the bottom of `run.go`:

```go
func autopilotPRTitle(rep *orchestrator.RunReport) string {
	if len(rep.Converged) == 1 {
		return fmt.Sprintf("aios: %s", rep.Converged[0])
	}
	return fmt.Sprintf("aios: %d converged tasks", len(rep.Converged))
}

func autopilotPRBody(rep *orchestrator.RunReport, runRoot string) string {
	var b strings.Builder
	b.WriteString("Autopilot run.\n\n")
	b.WriteString("**Converged tasks:**\n")
	for _, id := range rep.Converged {
		fmt.Fprintf(&b, "- %s\n", id)
	}
	if len(rep.Blocked) > 0 {
		b.WriteString("\n**Blocked or abandoned:**\n")
		for id, reason := range rep.Blocked {
			fmt.Fprintf(&b, "- %s — %s\n", id, reason.String())
		}
	}
	fmt.Fprintf(&b, "\nFull audit trail: `%s`\n", runRoot)
	b.WriteString("\nGenerated by AIOS autopilot.")
	return b.String()
}

func writeAutopilotSummary(rec *run.Recorder, fres *finalizerResult, finalizerErr error) error {
	var b strings.Builder
	b.WriteString("# Autopilot summary\n\n")
	if fres == nil {
		b.WriteString("Finalizer did not run.\n")
		return rec.WriteFile("autopilot-summary.md", []byte(b.String()))
	}
	if fres.SkipReason != "" {
		fmt.Fprintf(&b, "Skipped: %s\n", fres.SkipReason)
	} else if fres.PR != nil {
		fmt.Fprintf(&b, "PR: %s\n", fres.PR.URL)
		fmt.Fprintf(&b, "Checks: %s\n", fres.State)
		fmt.Fprintf(&b, "Merged: %v\n", fres.Merged)
	}
	if finalizerErr != nil {
		fmt.Fprintf(&b, "\nError: %v\n", finalizerErr)
	}
	return rec.WriteFile("autopilot-summary.md", []byte(b.String()))
}
```

- [ ] **Step 4: Run build + existing tests**

Run: `go build ./...`
Expected: clean.

Run: `go vet ./...`
Expected: clean.

Run: `go test ./...`
Expected: PASS — existing tests stay green; the autopilot/merge wiring is opt-in.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/run.go
git commit -m "feat(cli): wire autopilot preflight + finalizer into aios run"
```

---

## Task 11: Add `aios autopilot` top-level command

**Files:**
- Create: `internal/cli/autopilot.go`
- Modify: `internal/cli/root.go`

`aios autopilot "<idea>"` is the user-facing single command. It runs `runNew(NewOpts{Idea, Auto: true})`, then invokes `runMain` with `--autopilot --merge` set in flag state. Cleanest implementation: build a transient `*cobra.Command` with the right flags and call `runMain` against it.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/autopilot_test.go` (new file):

```go
package cli

import (
	"strings"
	"testing"
)

// TestAutopilotCmdHelpMentionsZeroIntervention is a smoke test: the help text
// should be discoverable so users grasp the contract from `aios autopilot --help`.
func TestAutopilotCmdHelpMentionsZeroIntervention(t *testing.T) {
	c := newAutopilotCmd()
	help := c.Long
	if !strings.Contains(help, "PR") || !strings.Contains(help, "merge") {
		t.Errorf("autopilot help should mention PR and merge; got: %q", help)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestAutopilotCmdHelpMentionsZeroIntervention -v`
Expected: FAIL — `undefined: newAutopilotCmd`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/autopilot.go`:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newAutopilotCmd is the user-facing single command:
//
//	aios autopilot "<idea>"
//
// It runs `aios new --auto` then `aios run --autopilot --merge` end-to-end
// with no human prompts. Equivalent to invoking those two commands by hand,
// minus the confirm gate and minus the manual `git merge`.
func newAutopilotCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "autopilot <idea>",
		Short: "Run new+run end-to-end with no prompts; open PR, wait for CI, squash-merge",
		Long: `Drives the full AIOS lifecycle for one idea with zero human input:

  1. brainstorm + spec-synth + decompose (no confirmation prompt)
  2. coder↔reviewer loop per task with verify+escalation
  3. open PR aios/staging→main, poll GitHub Actions, squash-merge on green

Stalled tasks are abandoned (audit trail under .aios/runs/<id>/abandoned/<task>/)
so a single bad task does not block the rest of the run. CI red or timeout
leaves the PR open without merging — the URL is printed and the run exits 2.

Requires: gh CLI on PATH, an authenticated gh session, and a configured git remote.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idea := strings.Join(args, " ")
			if err := runNew(NewOpts{Idea: idea, Auto: true}); err != nil {
				return fmt.Errorf("aios new (auto): %w", err)
			}
			// Build a transient run command with --autopilot and --merge set,
			// then invoke its RunE. This reuses the existing flag plumbing
			// without duplicating runMain's body.
			runCmd := newRunCmd()
			_ = runCmd.Flags().Set("autopilot", "true")
			_ = runCmd.Flags().Set("merge", "true")
			return runMain(runCmd, nil)
		},
	}
	return c
}
```

Modify `internal/cli/root.go`. Find:

```go
	root.AddCommand(newRunCmd())
	return root
```

Replace with:

```go
	root.AddCommand(newRunCmd())
	root.AddCommand(newAutopilotCmd())
	return root
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestAutopilotCmdHelpMentionsZeroIntervention -v`
Expected: PASS.

Run: `go build ./... && aios --help`
Expected: build clean; `aios autopilot` listed under Available Commands.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/autopilot.go internal/cli/autopilot_test.go internal/cli/root.go
git commit -m "feat(cli): aios autopilot orchestrates new+run with auto/autopilot/merge"
```

---

## Task 12: Integration test — autopilot finalizer happy path

**Files:**
- Create: `test/integration/autopilot_oneshot_test.go`

This test follows the pattern established by `test/integration/run_happy_test.go`: it drives `orchestrator.Run` directly with fake engines against a real git repo, then exercises `runAutopilotFinalizer` with a `githost.FakeHost`. **No refactor of `runMain` is required** — the existing integration tests don't go through Cobra either; they call orchestrator/worktree primitives directly. We do the same.

The autopilot CLI wiring (Task 10) is covered by unit tests + a manual smoke step in the "Done when" section. Automated end-to-end coverage of the full `aios autopilot` subprocess invocation is deferred to a follow-up issue (it requires building the binary inside the test, which is heavier than what this plan should carry).

- [ ] **Step 1: Write the failing test**

Create `test/integration/autopilot_oneshot_test.go`:

```go
package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/MoonCodeMaster/AIOS/internal/verify"
	"github.com/MoonCodeMaster/AIOS/internal/worktree"
)

// TestAutopilotOneShot_HappyPath drives one trivially-converging task through
// the orchestrator with FakeEngines, then runs the autopilot finalizer with
// a green FakeHost. Asserts: PR opened, MergePR called.
func TestAutopilotOneShot_HappyPath(t *testing.T) {
	repo := seedRepo(t)

	approve := `{"approved":true,"criteria":[{"id":"c1","status":"satisfied"}],"issues":[]}`
	coder := &engine.FakeEngine{Name_: "claude",
		Script: []engine.InvokeResponse{{Text: "coded"}}}
	reviewer := &engine.FakeEngine{Name_: "codex",
		Script: []engine.InvokeResponse{{Text: approve}}}

	wm := &worktree.Manager{RepoDir: repo, Root: filepath.Join(repo, ".aios", "worktrees")}
	task := &spec.Task{ID: "001-a", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}

	wt, err := wm.Create(task.ID, "aios/staging")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(wt.Path, "hello.txt"), []byte("hi\n"), 0o644)

	dep := &orchestrator.Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier: stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		Diff: func() (string, error) { return wm.Diff(wt, "aios/staging") },
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}
	out, err := orchestrator.Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != orchestrator.StateConverged {
		t.Fatalf("orchestrator final = %s", out.Final)
	}

	g := &worktree.Git{Dir: wt.Path}
	if _, err := g.Run("add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "aios: converged task 001-a"); err != nil {
		t.Fatal(err)
	}
	if err := wm.MergeFF(wt, "aios/staging"); err != nil {
		t.Fatal(err)
	}

	// Now exercise the finalizer.
	host := &githost.FakeHost{ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksGreen}}
	rec, err := run.Open(filepath.Join(repo, ".aios", "runs"), "test-run")
	if err != nil {
		t.Fatal(err)
	}
	res, err := cli.RunAutopilotFinalizerForTest(context.Background(), cli.FinalizerOptsForTest{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		Title:          "aios: 001-a",
		Body:           "test body",
		ConvergedCount: 1,
		ChecksTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	if res.PR == nil {
		t.Fatal("expected PR opened")
	}
	if !host.Merged[res.PR.Number] {
		t.Errorf("PR #%d should be merged", res.PR.Number)
	}
	if !res.Merged {
		t.Error("finalizer result should mark Merged=true")
	}

	// Sanity: writing the autopilot summary works.
	if err := cli.WriteAutopilotSummaryForTest(rec, res, nil); err != nil {
		t.Fatalf("WriteAutopilotSummary: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(rec.Root(), "autopilot-summary.md"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(body), "Merged: true") {
		t.Errorf("summary missing 'Merged: true': %s", body)
	}
	_ = wm.Remove(wt)
}
```

The `cli.RunAutopilotFinalizerForTest` and `cli.WriteAutopilotSummaryForTest` exports are added in Step 3 — `runAutopilotFinalizer` and `writeAutopilotSummary` are package-private. Rather than make them public (which would pollute the CLI surface), expose thin test-only wrappers.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/integration/... -run TestAutopilotOneShot_HappyPath -v`
Expected: FAIL — `undefined: cli.RunAutopilotFinalizerForTest`, `undefined: cli.FinalizerOptsForTest`, `undefined: cli.WriteAutopilotSummaryForTest`.

- [ ] **Step 3: Add test-only export shims**

Create `internal/cli/export_test.go`:

```go
package cli

import (
	"context"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/MoonCodeMaster/AIOS/internal/run"
)

// FinalizerOptsForTest mirrors the unexported finalizerOpts so external tests
// (test/integration/...) can build inputs for the finalizer without the
// finalizer's internals leaking into the public CLI surface.
type FinalizerOptsForTest = finalizerOpts

// FinalizerResultForTest is the test-visible alias for finalizerResult.
type FinalizerResultForTest = finalizerResult

// RunAutopilotFinalizerForTest exposes the unexported runAutopilotFinalizer
// to the integration test suite. Production code should not depend on this.
func RunAutopilotFinalizerForTest(ctx context.Context, opts FinalizerOptsForTest) (*FinalizerResultForTest, error) {
	return runAutopilotFinalizer(ctx, opts)
}

// WriteAutopilotSummaryForTest exposes writeAutopilotSummary to integration tests.
func WriteAutopilotSummaryForTest(rec *run.Recorder, res *FinalizerResultForTest, finalizerErr error) error {
	return writeAutopilotSummary(rec, res, finalizerErr)
}

// Suppress unused-import warnings if a future refactor drops one of these.
var _ = githost.NewCLIHost
```

The `_test.go` suffix on the file name means it is only compiled into the cli package's own test binary — but a type alias declared in a `_test.go` file is *not* visible to other packages. To make these visible to `test/integration/`, the file must be a normal `.go` file (no `_test` suffix). Rename to `internal/cli/export_for_tests.go` and gate it with a build tag:

Actually the simplest answer: don't gate it; keep it as a normal exported file. The two symbols are clearly named (`...ForTest`), zero production callers reference them, and the type aliases avoid duplicating the structs. This is a known Go idiom for test-only exports from internal packages.

Final form: create `internal/cli/export_for_tests.go` (without the `_test` suffix, no build tag):

```go
package cli

import (
	"context"

	"github.com/MoonCodeMaster/AIOS/internal/run"
)

// FinalizerOptsForTest mirrors finalizerOpts for external test packages.
type FinalizerOptsForTest = finalizerOpts

// FinalizerResultForTest mirrors finalizerResult for external test packages.
type FinalizerResultForTest = finalizerResult

// RunAutopilotFinalizerForTest is a test-only entry point. Production code
// must not depend on it (enforced socially; lint rules can flag the suffix).
func RunAutopilotFinalizerForTest(ctx context.Context, opts FinalizerOptsForTest) (*FinalizerResultForTest, error) {
	return runAutopilotFinalizer(ctx, opts)
}

// WriteAutopilotSummaryForTest is a test-only entry point.
func WriteAutopilotSummaryForTest(rec *run.Recorder, res *FinalizerResultForTest, finalizerErr error) error {
	return writeAutopilotSummary(rec, res, finalizerErr)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./test/integration/... -run TestAutopilotOneShot_HappyPath -v`
Expected: PASS.

Run: `go test ./...`
Expected: PASS — full suite.

- [ ] **Step 5: Commit**

```bash
git add test/integration/autopilot_oneshot_test.go internal/cli/export_for_tests.go
git commit -m "test(integration): autopilot finalizer happy path with fake githost"
```

---

## Task 13: Integration test — abandoned-task artifact written end-to-end

**Files:**
- Create: `test/integration/autopilot_abandoned_test.go`

This test exercises `run.WriteAbandoned` against a real `*run.Recorder` and a synthesized `orchestrator.Outcome` representing a stalled task. Asserts the artifact layout matches what an autopilot run will produce. Full end-to-end "stall in orchestrator → abandon write → finalizer continues" coverage requires the `runMain` wiring to be tested via subprocess invocation, which is deferred (see Task 12 note).

- [ ] **Step 1: Write the failing test**

Create `test/integration/autopilot_abandoned_test.go`:

```go
package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/run"
)

func TestAutopilot_AbandonedArtifact_LayoutEndToEnd(t *testing.T) {
	dir := t.TempDir()
	rec, err := run.Open(dir, "run-id")
	if err != nil {
		t.Fatal(err)
	}

	info := run.AbandonedInfo{
		TaskID:    "004-rescue",
		Reason:    "stall_no_progress: 3 consecutive rounds raised identical review issues",
		BlockCode: "stall_no_progress",
		UsageTokens: 12_345,
		Rounds: []run.AbandonedRound{
			{N: 1, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
			{N: 2, ReviewApproved: false, IssueCount: 3, VerifyGreen: false},
			{N: 3, ReviewApproved: false, IssueCount: 3, VerifyGreen: false, Escalated: true},
		},
	}
	if err := run.WriteAbandoned(rec, info); err != nil {
		t.Fatalf("WriteAbandoned: %v", err)
	}

	reportPath := filepath.Join(rec.Root(), "abandoned", "004-rescue", "report.md")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("report.md missing: %v", err)
	}
	body, _ := os.ReadFile(reportPath)
	for _, want := range []string{"004-rescue", "stall_no_progress", "12345"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("report.md missing %q; got: %s", want, body)
		}
	}

	jsonPath := filepath.Join(rec.Root(), "abandoned", "004-rescue", "full-trail.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("full-trail.json missing: %v", err)
	}
	var roundtrip run.AbandonedInfo
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatalf("full-trail.json invalid JSON: %v", err)
	}
	if roundtrip.TaskID != "004-rescue" {
		t.Errorf("roundtrip TaskID = %q, want %q", roundtrip.TaskID, "004-rescue")
	}
	if len(roundtrip.Rounds) != 3 || !roundtrip.Rounds[2].Escalated {
		t.Errorf("roundtrip rounds = %+v, want 3 with last escalated", roundtrip.Rounds)
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./test/integration/... -run TestAutopilot_AbandonedArtifact -v`
Expected: PASS — uses `run.WriteAbandoned` from Task 6.

- [ ] **Step 3: Commit**

```bash
git add test/integration/autopilot_abandoned_test.go
git commit -m "test(integration): abandoned-task artifact layout end-to-end"
```

---

## Task 14: Integration test — CI red prevents merge

**Files:**
- Create: `test/integration/autopilot_ci_red_test.go`

This test exercises `cli.RunAutopilotFinalizerForTest` with a red FakeHost. Asserts: PR opened, MergePR never called, finalizer returns an error citing "checks ended red".

- [ ] **Step 1: Write the failing test**

Create `test/integration/autopilot_ci_red_test.go`:

```go
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestAutopilot_CIRed_LeavesPROpenAndDoesNotMerge(t *testing.T) {
	host := &githost.FakeHost{ChecksByPR: map[int]githost.ChecksState{1: githost.ChecksRed}}

	res, err := cli.RunAutopilotFinalizerForTest(context.Background(), cli.FinalizerOptsForTest{
		Host:           host,
		Base:           "main",
		Head:           "aios/staging",
		Title:          "aios: 001-a",
		Body:           "test body",
		ConvergedCount: 1,
		ChecksTimeout:  time.Second,
	})
	if err == nil {
		t.Fatal("expected error on red checks")
	}
	if !strings.Contains(err.Error(), "red") {
		t.Errorf("error %q should mention 'red'", err)
	}
	if res == nil || res.PR == nil {
		t.Fatal("PR should still be reported on red so user has the URL")
	}
	if host.Merged[res.PR.Number] {
		t.Errorf("must not merge red PR #%d", res.PR.Number)
	}
	if len(host.OpenedPRs) != 1 {
		t.Errorf("expected exactly 1 PR opened, got %d", len(host.OpenedPRs))
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./test/integration/... -run TestAutopilot_CIRed -v`
Expected: PASS — relies on the test exports added in Task 12.

- [ ] **Step 3: Commit**

```bash
git add test/integration/autopilot_ci_red_test.go
git commit -m "test(integration): autopilot leaves PR open when CI is red"
```

---

## Task 15: Update README with `aios autopilot`

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a new "Autopilot" subsection under Quick start**

Find the existing "Quick start" section. After it, insert a new section:

```markdown
## Autopilot mode (zero human input)

For end-to-end runs with no prompts and no manual `git merge`:

```bash
cd your-repo
aios init
aios autopilot "Add a /health endpoint with a unit test"
# AIOS runs the full lifecycle: spec → tasks → coder↔reviewer → PR → CI → merge
# Stuck tasks abandon locally with a full audit trail; the rest of the run
# proceeds. CI red leaves the PR open and exits non-zero.
```

Requires: `gh` CLI authenticated (`gh auth login`), and your repo has a remote
configured. Stalled tasks land under `.aios/runs/<id>/abandoned/<task>/` for
later inspection — they never freeze the run.
```

- [ ] **Step 2: Update the "Project status" section**

Find the "Known limitations" list. Remove the line `- Auto-decompose for stuck tasks is not yet implemented; blocked tasks currently surface as [NEEDS HUMAN] for manual split.` (still partially true for non-autopilot runs; M2 closes it fully). Add the new caveat:

```markdown
- Auto-decompose for stuck tasks is shipping in v0.3.0; in autopilot mode (v0.2.0)
  stalled tasks are abandoned with a full audit trail rather than blocking the run.
- `--sandbox` (container isolation) remains stubbed; per-task `git worktree`
  isolation continues to be the v0.x story.
- MCP call failures are surfaced in audit logs; surfacing them inside the
  reviewer prompt is shipping in v0.3.1.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): document aios autopilot one-shot mode"
```

---

## Self-review checklist (run before declaring M1 done)

- [ ] **Spec coverage:** Every M1 component in `docs/superpowers/specs/2026-04-25-autopilot-roadmap-design.md` § M1 has a Task above. Components verified: `aios autopilot` command (Task 11), `aios new --auto` (Task 5), `aios run --autopilot --merge` (Tasks 8, 9, 10), `internal/githost/` (Tasks 1–4), autopilot finalizer (Tasks 9, 10), abandoned-task handler (Task 6), `stallThreshold` config wiring (already done in upstream — verified in this plan's File Structure section).
- [ ] **TDD discipline:** Every code-producing task starts with a failing test, makes it pass, then commits.
- [ ] **No placeholders:** Every step has either complete code or a precise `git`/`go` invocation. The word "placeholder" appears only in Tasks 1–4 to describe the throwaway stub structs that exist between the interface declaration (Task 1) and the real implementations (Tasks 2–4); they are not plan-failure placeholders.
- [ ] **Type consistency:** `NewOpts.Auto`, `RunOpts.Autopilot`, `finalizerOpts.ConvergedCount`, `AbandonedInfo.TaskID`, `githost.PR.Number` are referenced consistently across tasks.
- [ ] **Frequent commits:** Every task ends with a single commit; commit messages follow the `feat(scope):` / `test(scope):` / `docs(scope):` convention already used in the repo.
- [ ] **Build green at every commit:** Each task's final step runs `go build ./...` and the relevant test target before committing.
- [ ] **Existing behaviour preserved:** All new flags (`--auto`, `--autopilot`, `--merge`) are opt-in. `aios new` without `--auto` still prompts; `aios run` without `--autopilot` still blocks on stall; nothing about the legacy interactive flow changes.

---

## Out of scope for this plan (deferred to later milestones)

- **Auto-decompose** (M2 plan, after M1 ships).
- **MCP-failure → reviewer prompt** (M3).
- **`aios serve` issue-bot** (M4 plan, after M2/M3 ship).
- **`--sandbox` Docker isolation** (post-1.0).
- **Resuming an interrupted autopilot run** — partial-run safety is already a property of the existing pipeline (`aios/staging` is preserved across runs); explicit `--resume` is a separate feature.

---

## Done when

- All 15 tasks above are merged.
- `go test ./...` passes (unit + integration).
- A manual smoke run of `aios autopilot "test feature"` against a throwaway repo opens a PR, waits for CI, fast-forwards `main`, exits 0.
- Tag `v0.2.0` cut and published to npm via the existing release workflow.
- A first AIOS-on-AIOS PR is observed in the AIOS repo (this is the dogfood detour from the roadmap; the natural candidate is M2's first sub-task once the M2 plan is written).
