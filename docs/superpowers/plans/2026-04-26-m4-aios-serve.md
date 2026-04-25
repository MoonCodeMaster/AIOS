# M4 — `aios serve` issue-bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A daemon (or `--once` cron) that watches a GitHub repo for `aios:do`-labeled issues, runs autopilot per issue, opens PRs, and files `aios:stuck` issues with the audit trail on abandon. Single-repo, sequential by default. Targets release **v0.5.0**.

**Architecture:** `aios serve` polls GitHub via the existing `gh` CLI (extending `internal/githost` with issue operations). For each picked-up issue, a per-issue runner shells out to `aios autopilot "<title + body>"` as a subprocess, parses the resulting `autopilot-summary.md` to determine outcome, and applies a label state machine on the issue. State is persisted to `.aios/serve/state.json` so a killed daemon reconciles cleanly on restart.

**Tech Stack:** Go 1.26.2, `gh` CLI, TOML config, JSON state file, subprocess invocation via `os/exec`.

**Spec reference:** `docs/superpowers/specs/2026-04-25-autopilot-roadmap-design.md` § M4.

---

## Scope cut for v0.5.0

The spec lists `max_concurrent_issues = 2` as the default. **For v0.5.0 we ship sequential-only (concurrency = 1)** and call this out explicitly. Reasons:

- `aios new --auto` and `aios run` both write to `.aios/project.md` and `.aios/tasks/` — concurrent autopilot runs in the same repo collide on those paths.
- True concurrency would need per-issue subdirectory isolation in `.aios/`, which is a larger refactor.
- The single-developer dogfood case is well-served by sequential runs.

The config knob exists in M4 (`[concurrency] max_concurrent_issues`) but the runner reads it and clamps to 1 with a warning if anything higher is configured. Concurrency >1 is a follow-up.

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `internal/githost/issues.go` | Issue type + `ListLabeled` / `AddLabel` / `RemoveLabel` / `AddComment` / `OpenIssue` / `CloseIssue` methods on the existing `Host` interface. CLIHost real impl via `gh` subprocess. |
| `internal/githost/issues_test.go` | Unit tests for CLIHost issue methods using the existing `fakeExec` pattern. |
| `internal/githost/fake.go` (modify) | Extend `FakeHost` with issue state and method implementations. Update guard and tests. |
| `internal/cli/serve_config.go` | Reads `.aios/serve.toml`. Defines `ServeConfig`, defaults. |
| `internal/cli/serve_config_test.go` | Round-trip + defaults. |
| `internal/cli/serve_state.go` | Persists `.aios/serve/state.json`. `Save` / `Load` / `Reconcile(host)` methods. |
| `internal/cli/serve_state_test.go` | Reconcile drift cases (label-only, state-only, both, neither). |
| `internal/cli/serve_runner.go` | Per-issue runner — `RunIssue(ctx, host, autopilotFn, issue) (Outcome, error)`. Label state machine. |
| `internal/cli/serve_runner_test.go` | Each outcome path with FakeHost + injected autopilotFn. |
| `internal/cli/serve.go` | `aios serve [--once] [--repo OWNER/NAME]` Cobra command. Poll loop, sequential dispatch, startup reconcile. |
| `internal/cli/serve_test.go` | Help-text smoke test only — full behaviour covered by `serve_runner_test.go` + integration tests. |
| `test/integration/serve_oneshot_test.go` | Three outcome scenarios (merged / abandoned / pr-open-red) with a FakeHost end-to-end. |
| `test/integration/serve_recovery_test.go` | Reconcile after a kill — orphan label released back to do-set. |

**Modified files:**

| Path | Change |
|---|---|
| `internal/githost/githost.go` | Add issue methods to the `Host` interface. Add `Issue` struct. |
| `internal/cli/root.go` | Register `newServeCmd()`. |
| `README.md` | New "Serve mode (issue bot)" subsection. Project status update. |
| `docs/architecture.md` | New "Serve mode (`internal/cli/serve.go`)" component subsection. |

---

## Implementation order

1. Tasks 1: extend `Host` with issue operations (foundation).
2. Tasks 2: serve config + state + reconcile (data layer, independent of runner).
3. Task 3: per-issue runner (the workhorse).
4. Task 4: serve command (poll loop, ties everything together).
5. Task 5: integration tests across all outcome paths.
6. Task 6: docs.

---

## Task 1: Issue operations on `Host`

**Files:**
- Modify: `internal/githost/githost.go`
- Create: `internal/githost/issues.go`
- Create: `internal/githost/issues_test.go`
- Modify: `internal/githost/fake.go`
- Modify: `internal/githost/fake_test.go`

Extend the `Host` interface with six issue operations. CLIHost implements via the `gh` CLI; FakeHost implements in-memory.

- [ ] **Step 1: Add `Issue` struct + interface methods in `githost.go`**

Append to `internal/githost/githost.go`:

```go
// Issue identifies a GitHub issue.
type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
	URL    string
}
```

Update the `Host` interface to add the issue methods (place after `MergePR`):

```go
	// ListLabeled returns issues currently carrying the given label. The
	// label is matched exactly. Closed issues are excluded.
	ListLabeled(ctx context.Context, label string) ([]Issue, error)

	// AddLabel adds the label to the issue. No-op if already present.
	AddLabel(ctx context.Context, issueNum int, label string) error

	// RemoveLabel removes the label from the issue. No-op if not present.
	RemoveLabel(ctx context.Context, issueNum int, label string) error

	// AddComment posts a comment on the issue.
	AddComment(ctx context.Context, issueNum int, body string) error

	// OpenIssue creates a new issue and returns its number.
	OpenIssue(ctx context.Context, title, body string, labels []string) (int, error)

	// CloseIssue closes the issue (state = "closed").
	CloseIssue(ctx context.Context, issueNum int) error
```

- [ ] **Step 2: Write the failing tests for CLIHost issue ops**

Create `internal/githost/issues_test.go`:

```go
package githost

import (
	"context"
	"reflect"
	"testing"
)

func TestCLIHost_ListLabeled_ParsesGhJSON(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec(`[{"number":42,"title":"Add /health","body":"endpoint","labels":[{"name":"aios:do"},{"name":"good first issue"}],"url":"https://example.invalid/issues/42"}]`, 0),
	}
	issues, err := host.ListLabeled(context.Background(), "aios:do")
	if err != nil {
		t.Fatalf("ListLabeled: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 42 || issues[0].Title != "Add /health" {
		t.Errorf("ListLabeled = %+v, want one issue #42 'Add /health'", issues)
	}
	if !reflect.DeepEqual(issues[0].Labels, []string{"aios:do", "good first issue"}) {
		t.Errorf("Labels = %v, want [aios:do good first issue]", issues[0].Labels)
	}
}

func TestCLIHost_AddLabel_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.AddLabel(context.Background(), 42, "aios:in-progress"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	want := []string{"gh", "issue", "edit", "42", "--add-label", "aios:in-progress"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_RemoveLabel_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.RemoveLabel(context.Background(), 42, "aios:do"); err != nil {
		t.Fatalf("RemoveLabel: %v", err)
	}
	want := []string{"gh", "issue", "edit", "42", "--remove-label", "aios:do"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_AddComment_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.AddComment(context.Background(), 42, "merged in #99"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	want := []string{"gh", "issue", "comment", "42", "--body", "merged in #99"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}

func TestCLIHost_OpenIssue_ParsesNewIssueURL(t *testing.T) {
	host := &CLIHost{exec: fakeExec("https://github.com/owner/repo/issues/77\n", 0)}
	num, err := host.OpenIssue(context.Background(), "title", "body", []string{"aios:stuck"})
	if err != nil {
		t.Fatalf("OpenIssue: %v", err)
	}
	if num != 77 {
		t.Errorf("OpenIssue number = %d, want 77", num)
	}
}

func TestCLIHost_CloseIssue_InvocationShape(t *testing.T) {
	var captured []string
	host := &CLIHost{exec: func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExec("", 0)(name, args...)
	}}
	if err := host.CloseIssue(context.Background(), 42); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	want := []string{"gh", "issue", "close", "42"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("invocation = %v, want %v", captured, want)
	}
}
```

Add `"os/exec"` to the imports if needed (it's already used in `cli_test.go`).

- [ ] **Step 3: Run tests — should fail**

Run: `go test ./internal/githost/... -run "TestCLIHost_(List|Add|Remove|Open|Close)" -v`
Expected: FAIL — methods not defined.

- [ ] **Step 4: Implement issue methods on CLIHost**

Create `internal/githost/issues.go`:

```go
package githost

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ghIssueJSON matches the subset of `gh issue list/view --json` output we use.
type ghIssueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (h *CLIHost) ListLabeled(ctx context.Context, label string) ([]Issue, error) {
	cmd := h.cmd(ctx, "gh", "issue", "list",
		"--label", label,
		"--state", "open",
		"--json", "number,title,body,url,labels",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	var raw []ghIssueJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh issue list: parse json: %w", err)
	}
	out2 := make([]Issue, 0, len(raw))
	for _, r := range raw {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out2 = append(out2, Issue{
			Number: r.Number,
			Title:  r.Title,
			Body:   r.Body,
			URL:    r.URL,
			Labels: labels,
		})
	}
	return out2, nil
}

func (h *CLIHost) AddLabel(ctx context.Context, issueNum int, label string) error {
	cmd := h.cmd(ctx, "gh", "issue", "edit", strconv.Itoa(issueNum), "--add-label", label)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue edit --add-label: %w", err)
	}
	return nil
}

func (h *CLIHost) RemoveLabel(ctx context.Context, issueNum int, label string) error {
	cmd := h.cmd(ctx, "gh", "issue", "edit", strconv.Itoa(issueNum), "--remove-label", label)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue edit --remove-label: %w", err)
	}
	return nil
}

func (h *CLIHost) AddComment(ctx context.Context, issueNum int, body string) error {
	cmd := h.cmd(ctx, "gh", "issue", "comment", strconv.Itoa(issueNum), "--body", body)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue comment: %w", err)
	}
	return nil
}

func (h *CLIHost) OpenIssue(ctx context.Context, title, body string, labels []string) (int, error) {
	args := []string{"issue", "create", "--title", title, "--body", body}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	cmd := h.cmd(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("gh issue create: %w", err)
	}
	url := strings.TrimSpace(string(out))
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("gh issue create: empty output")
	}
	num, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, fmt.Errorf("gh issue create: cannot parse issue number from %q: %w", url, err)
	}
	return num, nil
}

func (h *CLIHost) CloseIssue(ctx context.Context, issueNum int) error {
	cmd := h.cmd(ctx, "gh", "issue", "close", strconv.Itoa(issueNum))
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue close: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Extend FakeHost with issue state**

Modify `internal/githost/fake.go` to add issue tracking. Append to the struct:

```go
type FakeHost struct {
	mu     sync.Mutex
	nextID int
	prs    map[int]*PR

	ChecksByPR map[int]ChecksState
	Merged     map[int]bool
	OpenedPRs  []*PR

	// Issue state.
	Issues          []Issue           // issues seeded by tests
	NextIssueNumber int               // counter for OpenIssue
	OpenedIssues    []Issue           // issues created via OpenIssue
	Comments        map[int][]string  // per-issue comment bodies
	Closed          map[int]bool      // issues closed via CloseIssue
}
```

Add the methods at the end of `fake.go`:

```go
func (f *FakeHost) ListLabeled(_ context.Context, label string) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Issue
	for _, i := range f.Issues {
		for _, l := range i.Labels {
			if l == label {
				out = append(out, i)
				break
			}
		}
	}
	return out, nil
}

func (f *FakeHost) AddLabel(_ context.Context, issueNum int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, iss := range f.Issues {
		if iss.Number != issueNum {
			continue
		}
		for _, existing := range iss.Labels {
			if existing == label {
				return nil
			}
		}
		f.Issues[i].Labels = append(iss.Labels, label)
		return nil
	}
	return fmt.Errorf("FakeHost: issue %d not found for AddLabel", issueNum)
}

func (f *FakeHost) RemoveLabel(_ context.Context, issueNum int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, iss := range f.Issues {
		if iss.Number != issueNum {
			continue
		}
		out := iss.Labels[:0]
		for _, existing := range iss.Labels {
			if existing == label {
				continue
			}
			out = append(out, existing)
		}
		f.Issues[i].Labels = out
		return nil
	}
	return fmt.Errorf("FakeHost: issue %d not found for RemoveLabel", issueNum)
}

func (f *FakeHost) AddComment(_ context.Context, issueNum int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Comments == nil {
		f.Comments = map[int][]string{}
	}
	f.Comments[issueNum] = append(f.Comments[issueNum], body)
	return nil
}

func (f *FakeHost) OpenIssue(_ context.Context, title, body string, labels []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NextIssueNumber == 0 {
		f.NextIssueNumber = 1000 // start fake issues at 1000 to avoid collision with seeded
	}
	n := f.NextIssueNumber
	f.NextIssueNumber++
	iss := Issue{Number: n, Title: title, Body: body, Labels: labels, URL: fmt.Sprintf("https://example.invalid/issues/%d", n)}
	f.Issues = append(f.Issues, iss)
	f.OpenedIssues = append(f.OpenedIssues, iss)
	return n, nil
}

func (f *FakeHost) CloseIssue(_ context.Context, issueNum int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Closed == nil {
		f.Closed = map[int]bool{}
	}
	f.Closed[issueNum] = true
	return nil
}
```

- [ ] **Step 6: Run tests — should pass**

Run: `go test ./internal/githost/... -v`
Expected: PASS — all CLIHost issue tests + the existing PR tests + FakeHost tests.

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/githost/
git commit -m "feat(githost): issue operations (list, label, comment, open, close)"
```

---

## Task 2: Serve config + state + reconcile

**Files:**
- Create: `internal/cli/serve_config.go`
- Create: `internal/cli/serve_config_test.go`
- Create: `internal/cli/serve_state.go`
- Create: `internal/cli/serve_state_test.go`

Two small files for the data layer. Config from TOML, state from JSON, reconcile resolves drift between GitHub labels and local state.

- [ ] **Step 1: Write failing tests for serve_config**

Create `internal/cli/serve_config_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServeConfig_Defaults_WhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadServeConfig(filepath.Join(dir, "serve.toml"))
	if err != nil {
		t.Fatalf("LoadServeConfig (missing file): %v", err)
	}
	if cfg.Labels.Do != "aios:do" {
		t.Errorf("Labels.Do = %q, want aios:do", cfg.Labels.Do)
	}
	if cfg.Poll.IntervalSec != 60 {
		t.Errorf("Poll.IntervalSec = %d, want 60", cfg.Poll.IntervalSec)
	}
	if cfg.Concurrency.MaxConcurrentIssues != 1 {
		t.Errorf("Concurrency = %d, want 1 (sequential default for v0.5.0)", cfg.Concurrency.MaxConcurrentIssues)
	}
}

func TestServeConfig_LoadsTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.toml")
	body := `
[repo]
owner = "MoonCodeMaster"
name = "AIOS"

[labels]
do = "aios:please-do"
in_progress = "aios:wip"

[poll]
interval_sec = 30
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServeConfig(path)
	if err != nil {
		t.Fatalf("LoadServeConfig: %v", err)
	}
	if cfg.Repo.Owner != "MoonCodeMaster" || cfg.Repo.Name != "AIOS" {
		t.Errorf("Repo = %+v, want MoonCodeMaster/AIOS", cfg.Repo)
	}
	if cfg.Labels.Do != "aios:please-do" {
		t.Errorf("Labels.Do = %q, want aios:please-do", cfg.Labels.Do)
	}
	if cfg.Labels.InProgress != "aios:wip" {
		t.Errorf("Labels.InProgress = %q, want aios:wip", cfg.Labels.InProgress)
	}
	if cfg.Poll.IntervalSec != 30 {
		t.Errorf("Poll.IntervalSec = %d, want 30", cfg.Poll.IntervalSec)
	}
}
```

- [ ] **Step 2: Implement serve_config.go**

Create `internal/cli/serve_config.go`:

```go
package cli

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

// ServeConfig is the runtime config for `aios serve`. Read from
// .aios/serve.toml at startup. Unset fields default per the field comments.
type ServeConfig struct {
	Repo        ServeRepo        `toml:"repo"`
	Labels      ServeLabels      `toml:"labels"`
	Poll        ServePoll        `toml:"poll"`
	Concurrency ServeConcurrency `toml:"concurrency"`
}

type ServeRepo struct {
	Owner string `toml:"owner"`
	Name  string `toml:"name"`
}

type ServeLabels struct {
	Do         string `toml:"do"`          // default "aios:do"
	InProgress string `toml:"in_progress"` // default "aios:in-progress"
	PROpen     string `toml:"pr_open"`     // default "aios:pr-open"
	Stuck      string `toml:"stuck"`       // default "aios:stuck"
	Done       string `toml:"done"`        // default "aios:done"
}

type ServePoll struct {
	IntervalSec int `toml:"interval_sec"` // default 60
}

type ServeConcurrency struct {
	// MaxConcurrentIssues is clamped to 1 in v0.5.0. Higher values log a
	// warning and run sequentially.
	MaxConcurrentIssues int `toml:"max_concurrent_issues"`
}

// LoadServeConfig reads serve.toml from path. A missing file is not an error —
// defaults are used. A malformed file IS an error.
func LoadServeConfig(path string) (*ServeConfig, error) {
	cfg := &ServeConfig{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			applyServeDefaults(cfg)
			return cfg, nil
		}
		return nil, err
	}
	if _, err := toml.Decode(string(raw), cfg); err != nil {
		return nil, err
	}
	applyServeDefaults(cfg)
	return cfg, nil
}

func applyServeDefaults(c *ServeConfig) {
	if c.Labels.Do == "" {
		c.Labels.Do = "aios:do"
	}
	if c.Labels.InProgress == "" {
		c.Labels.InProgress = "aios:in-progress"
	}
	if c.Labels.PROpen == "" {
		c.Labels.PROpen = "aios:pr-open"
	}
	if c.Labels.Stuck == "" {
		c.Labels.Stuck = "aios:stuck"
	}
	if c.Labels.Done == "" {
		c.Labels.Done = "aios:done"
	}
	if c.Poll.IntervalSec == 0 {
		c.Poll.IntervalSec = 60
	}
	if c.Concurrency.MaxConcurrentIssues == 0 {
		c.Concurrency.MaxConcurrentIssues = 1
	}
}
```

- [ ] **Step 3: Run config tests**

Run: `go test ./internal/cli/... -run TestServeConfig -v`
Expected: PASS.

- [ ] **Step 4: Write failing tests for serve_state**

Create `internal/cli/serve_state_test.go`:

```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestServeState_RoundtripJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewServeState()
	s.Add(42, "run-id-1")
	s.Add(43, "run-id-2")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadServeState(path)
	if err != nil {
		t.Fatalf("LoadServeState: %v", err)
	}
	if len(loaded.Issues) != 2 || loaded.Issues[42].RunID != "run-id-1" {
		t.Errorf("loaded state mismatch: %+v", loaded.Issues)
	}
}

func TestServeState_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadServeState(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("LoadServeState (missing): %v", err)
	}
	if len(s.Issues) != 0 {
		t.Errorf("missing-file state should be empty, got %+v", s.Issues)
	}
}

func TestServeState_Reconcile_GitHubOnlyOrphan_ReleasesLabel(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 100, Title: "orphan", Labels: []string{"aios:in-progress"}},
	}}
	s := NewServeState() // empty — GitHub thinks #100 is in-progress, AIOS doesn't
	if err := s.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if labelsOf(host.Issues, 100) != "aios:do" {
		t.Errorf("issue #100 should have aios:do label after reconcile, got %v", host.Issues[0].Labels)
	}
}

func TestServeState_Reconcile_StateOnlyOrphan_RemovesFromState(t *testing.T) {
	host := &githost.FakeHost{} // no issues on GitHub
	s := NewServeState()
	s.Add(99, "orphan-run")
	if err := s.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, present := s.Issues[99]; present {
		t.Errorf("state-only orphan #99 should be removed; state = %+v", s.Issues)
	}
}

func labelsOf(issues []githost.Issue, num int) string {
	for _, i := range issues {
		if i.Number == num {
			if len(i.Labels) == 0 {
				return ""
			}
			return i.Labels[0] // assertions in tests look at first label
		}
	}
	return ""
}
```

- [ ] **Step 5: Implement serve_state.go**

Create `internal/cli/serve_state.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

// InProgressIssue is one issue that AIOS thinks it is currently working on.
type InProgressIssue struct {
	RunID     string    `json:"run_id"`
	ClaimedAt time.Time `json:"claimed_at"`
}

// ServeState is the on-disk record of which GitHub issues this `aios serve`
// process is currently working on. Persisted to .aios/serve/state.json so
// that a killed process can reconcile orphans on restart.
type ServeState struct {
	mu     sync.Mutex
	Issues map[int]InProgressIssue `json:"issues"`
}

func NewServeState() *ServeState { return &ServeState{Issues: map[int]InProgressIssue{}} }

func LoadServeState(path string) (*ServeState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewServeState(), nil
		}
		return nil, err
	}
	s := NewServeState()
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse serve state: %w", err)
	}
	if s.Issues == nil {
		s.Issues = map[int]InProgressIssue{}
	}
	return s, nil
}

func (s *ServeState) Save(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (s *ServeState) Add(issueNum int, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Issues[issueNum] = InProgressIssue{RunID: runID, ClaimedAt: time.Now()}
}

func (s *ServeState) Remove(issueNum int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Issues, issueNum)
}

// Reconcile resolves drift between the GitHub view (issues currently labeled
// `inProgressLabel`) and the local state.json view. Three cases:
//   - On both sides → leave alone (the in-flight run will resume or finish).
//   - Only on GitHub (label set, no local state) → release: remove the
//     in-progress label and re-add the do label so a future poll picks it up.
//   - Only locally (state entry, no label) → remove from state.
func (s *ServeState) Reconcile(ctx context.Context, host githost.Host, doLabel, inProgressLabel string) error {
	githubInflight, err := host.ListLabeled(ctx, inProgressLabel)
	if err != nil {
		return fmt.Errorf("reconcile list: %w", err)
	}
	githubSet := map[int]bool{}
	for _, i := range githubInflight {
		githubSet[i.Number] = true
	}
	s.mu.Lock()
	stateSet := map[int]bool{}
	for n := range s.Issues {
		stateSet[n] = true
	}
	s.mu.Unlock()

	// GitHub-only orphans → release.
	for n := range githubSet {
		if stateSet[n] {
			continue
		}
		if err := host.RemoveLabel(ctx, n, inProgressLabel); err != nil {
			return fmt.Errorf("reconcile remove %s on #%d: %w", inProgressLabel, n, err)
		}
		if err := host.AddLabel(ctx, n, doLabel); err != nil {
			return fmt.Errorf("reconcile add %s on #%d: %w", doLabel, n, err)
		}
	}
	// State-only orphans → drop.
	for n := range stateSet {
		if githubSet[n] {
			continue
		}
		s.Remove(n)
	}
	return nil
}
```

Add `"github.com/BurntSushi/toml"` to go.mod if not present (it already is, used by config).

- [ ] **Step 6: Run tests**

Run: `go test ./internal/cli/... -run "TestServeConfig|TestServeState" -v`
Expected: PASS.

Run: `go test ./...`
Expected: full suite green.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/serve_config.go internal/cli/serve_config_test.go internal/cli/serve_state.go internal/cli/serve_state_test.go
git commit -m "feat(serve): config (.aios/serve.toml) + state (.aios/serve/state.json) with reconcile"
```

---

## Task 3: Per-issue runner

**Files:**
- Create: `internal/cli/serve_runner.go`
- Create: `internal/cli/serve_runner_test.go`

The runner takes one issue, runs autopilot via an injected callback (production wires it to a subprocess), parses the autopilot summary, and applies the label state machine.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/serve_runner_test.go`:

```go
package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func TestServeRunner_Merged_LabelsAndCloses(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{Status: AutopilotMerged, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if !host.Closed[42] {
		t.Error("merged issue must be closed")
	}
	got := labelSetOf(host.Issues, 42)
	if got["aios:do"] {
		t.Error("aios:do should be removed after merge")
	}
	if !got["aios:done"] {
		t.Errorf("aios:done expected, labels = %v", got)
	}
	comments := host.Comments[42]
	if len(comments) == 0 || !strings.Contains(comments[0], "#99") {
		t.Errorf("expected merge comment referencing #99, got %v", comments)
	}
}

func TestServeRunner_Abandoned_OpensStuckIssue(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{Status: AutopilotAbandoned, AuditTrail: "trail content"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if len(host.OpenedIssues) != 1 {
		t.Fatalf("expected 1 stuck issue opened, got %d", len(host.OpenedIssues))
	}
	stuck := host.OpenedIssues[0]
	if !strings.HasPrefix(stuck.Title, "[aios:stuck]") {
		t.Errorf("stuck issue title = %q, want [aios:stuck] prefix", stuck.Title)
	}
	got := labelSetOf(host.Issues, 42)
	if !got["aios:stuck"] {
		t.Errorf("aios:stuck expected on original issue, labels = %v", got)
	}
	if host.Closed[42] {
		t.Error("abandoned issue should NOT be closed (waiting for human triage)")
	}
}

func TestServeRunner_PROpenRed_KeepsOpen(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{Status: AutopilotPRRed, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	got := labelSetOf(host.Issues, 42)
	if !got["aios:pr-open"] {
		t.Errorf("aios:pr-open expected, labels = %v", got)
	}
	if host.Closed[42] {
		t.Error("PR-red issue should NOT be closed")
	}
}

func TestServeRunner_AutopilotError_SurfacesAndReleases(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := NewServeState()
	cfg := &ServeConfig{}
	applyServeDefaults(cfg)

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: func(_ context.Context, _ string) (AutopilotResult, error) {
			return AutopilotResult{}, errors.New("autopilot binary not found")
		},
	}
	err := runner.RunIssue(context.Background(), host.Issues[0])
	if err == nil {
		t.Fatal("expected error from autopilot")
	}
	got := labelSetOf(host.Issues, 42)
	// On error, the issue should be released back to aios:do for retry.
	if got["aios:in-progress"] {
		t.Error("aios:in-progress should be removed on error")
	}
	if !got["aios:do"] {
		t.Error("aios:do should be re-added on error so the issue can be retried")
	}
}

func labelSetOf(issues []githost.Issue, num int) map[string]bool {
	m := map[string]bool{}
	for _, i := range issues {
		if i.Number == num {
			for _, l := range i.Labels {
				m[l] = true
			}
		}
	}
	return m
}
```

- [ ] **Step 2: Run tests — should fail**

Run: `go test ./internal/cli/... -run TestServeRunner -v`
Expected: FAIL — types not defined.

- [ ] **Step 3: Implement serve_runner.go**

Create `internal/cli/serve_runner.go`:

```go
package cli

import (
	"context"
	"fmt"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

// AutopilotStatus is the outcome of one autopilot run.
type AutopilotStatus int

const (
	AutopilotUnknown AutopilotStatus = iota
	AutopilotMerged                  // PR merged to main
	AutopilotPRRed                   // PR opened but CI red or timed out
	AutopilotAbandoned               // all tasks abandoned, no PR
)

// AutopilotResult is the parsed outcome of one autopilot subprocess run.
type AutopilotResult struct {
	Status     AutopilotStatus
	PRNumber   int
	PRURL      string
	AuditTrail string // body for the aios:stuck issue when Status == Abandoned
}

// AutopilotFn runs autopilot for one idea string and returns the parsed result.
// Production wiring shells out to `aios autopilot "<idea>"` and parses the
// resulting autopilot-summary.md. Tests inject fakes.
type AutopilotFn func(ctx context.Context, idea string) (AutopilotResult, error)

// ServeRunner ties together a host, state, config, and autopilot callback to
// drive one issue from claim to closure.
type ServeRunner struct {
	Host      githost.Host
	State     *ServeState
	Config    *ServeConfig
	Autopilot AutopilotFn
}

// RunIssue claims an issue, runs autopilot, applies the label state machine
// and final issue actions (comment, close, open stuck issue), and clears the
// state entry. On autopilot error, the issue is released back to aios:do for
// the next poll cycle to retry.
func (r *ServeRunner) RunIssue(ctx context.Context, issue githost.Issue) error {
	labels := r.Config.Labels
	if err := r.Host.RemoveLabel(ctx, issue.Number, labels.Do); err != nil {
		return fmt.Errorf("remove %s: %w", labels.Do, err)
	}
	if err := r.Host.AddLabel(ctx, issue.Number, labels.InProgress); err != nil {
		return fmt.Errorf("add %s: %w", labels.InProgress, err)
	}
	r.State.Add(issue.Number, fmt.Sprintf("issue-%d", issue.Number))

	idea := renderIdea(issue)
	result, err := r.Autopilot(ctx, idea)
	if err != nil {
		// Release the claim so the next poll picks the issue up again.
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.Do)
		r.State.Remove(issue.Number)
		return fmt.Errorf("autopilot: %w", err)
	}

	switch result.Status {
	case AutopilotMerged:
		_ = r.Host.AddComment(ctx, issue.Number, fmt.Sprintf("Merged in #%d (%s); closing.", result.PRNumber, result.PRURL))
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.Done)
		_ = r.Host.CloseIssue(ctx, issue.Number)
	case AutopilotPRRed:
		_ = r.Host.AddComment(ctx, issue.Number, fmt.Sprintf("PR #%d (%s) open; CI failing or timed out — needs human review.", result.PRNumber, result.PRURL))
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.PROpen)
	case AutopilotAbandoned:
		stuckBody := fmt.Sprintf("Original issue: #%d\n\nAutopilot abandoned after exhausted retries and decompose attempts.\n\n%s",
			issue.Number, result.AuditTrail)
		stuckNum, err := r.Host.OpenIssue(ctx, fmt.Sprintf("[aios:stuck] %s", issue.Title), stuckBody, []string{labels.Stuck})
		if err != nil {
			return fmt.Errorf("open stuck issue: %w", err)
		}
		_ = r.Host.AddComment(ctx, issue.Number, fmt.Sprintf("Couldn't converge; full audit trail in #%d.", stuckNum))
		_ = r.Host.RemoveLabel(ctx, issue.Number, labels.InProgress)
		_ = r.Host.AddLabel(ctx, issue.Number, labels.Stuck)
	default:
		return fmt.Errorf("autopilot returned unknown status %d", result.Status)
	}
	r.State.Remove(issue.Number)
	return nil
}

// renderIdea is the issue→idea-string converter. Per spec, verbatim title +
// body without LLM rewriting — pre-processing would mask context like code
// snippets or error logs in the issue body.
func renderIdea(issue githost.Issue) string {
	if issue.Body == "" {
		return issue.Title
	}
	return issue.Title + "\n\n" + issue.Body
}
```

- [ ] **Step 4: Run tests — should pass**

Run: `go test ./internal/cli/... -run TestServeRunner -v`
Expected: PASS — all four cases.

Run: `go test ./...`
Expected: full suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/serve_runner.go internal/cli/serve_runner_test.go
git commit -m "feat(serve): per-issue runner with label state machine"
```

---

## Task 4: `aios serve` command

**Files:**
- Create: `internal/cli/serve.go`
- Create: `internal/cli/serve_test.go`
- Modify: `internal/cli/root.go`

The Cobra command. Polls every `interval_sec`, dispatches one runner per pending issue (sequential — concurrency clamp), reconciles on startup, supports `--once` for cron use.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/serve_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestServeCmdHelpDescribesContract(t *testing.T) {
	c := newServeCmd()
	long := c.Long
	for _, want := range []string{"aios:do", "GitHub", "label", "--once"} {
		if !strings.Contains(long, want) {
			t.Errorf("serve --help missing %q; got: %s", want, long)
		}
	}
}
```

- [ ] **Step 2: Implement serve.go**

Create `internal/cli/serve.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/githost"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "serve",
		Short: "Watch GitHub issues with the aios:do label and run autopilot per issue",
		Long: `Watches a GitHub repo for issues labeled aios:do, runs autopilot per issue,
opens PRs, comments back, files aios:stuck issues with the audit trail on
abandon.

Sequential by default in v0.5.0 — one issue at a time. Concurrency >1 is
configured via .aios/serve.toml [concurrency] max_concurrent_issues but
clamped to 1 internally for now.

Modes:
  aios serve            Long-running daemon. Polls every interval_sec.
  aios serve --once     Single poll cycle, exit. For cron / GitHub Actions.

Requires: gh CLI authenticated (gh auth login). The repo to watch is read
from .aios/serve.toml [repo] owner/name; if absent, the current git repo's
default remote is used.`,
		RunE: runServe,
	}
	c.Flags().Bool("once", false, "single poll cycle, then exit (for cron)")
	c.Flags().String("repo", "", "OWNER/NAME (overrides .aios/serve.toml [repo])")
	return c
}

func runServe(cmd *cobra.Command, _ []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := LoadServeConfig(filepath.Join(wd, ".aios", "serve.toml"))
	if err != nil {
		return fmt.Errorf("load serve config: %w", err)
	}
	if repoFlag, _ := cmd.Flags().GetString("repo"); repoFlag != "" {
		parts := strings.SplitN(repoFlag, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("--repo must be OWNER/NAME, got %q", repoFlag)
		}
		cfg.Repo.Owner, cfg.Repo.Name = parts[0], parts[1]
	}
	if cfg.Concurrency.MaxConcurrentIssues > 1 {
		fmt.Fprintf(os.Stderr, "warn: max_concurrent_issues=%d clamped to 1 in v0.5.0\n", cfg.Concurrency.MaxConcurrentIssues)
		cfg.Concurrency.MaxConcurrentIssues = 1
	}

	statePath := filepath.Join(wd, ".aios", "serve", "state.json")
	state, err := LoadServeState(statePath)
	if err != nil {
		return fmt.Errorf("load serve state: %w", err)
	}
	host := githost.NewCLIHost()

	if err := state.Reconcile(cmd.Context(), host, cfg.Labels.Do, cfg.Labels.InProgress); err != nil {
		fmt.Fprintf(os.Stderr, "warn: reconcile failed: %v\n", err)
	}
	_ = state.Save(statePath)

	once, _ := cmd.Flags().GetBool("once")

	runner := &ServeRunner{
		Host: host, State: state, Config: cfg,
		Autopilot: subprocessAutopilot,
	}

	doOne := func(ctx context.Context) error {
		issues, err := host.ListLabeled(ctx, cfg.Labels.Do)
		if err != nil {
			return fmt.Errorf("list labeled: %w", err)
		}
		// Skip issues already in state (in-progress).
		for _, iss := range issues {
			if _, present := state.Issues[iss.Number]; present {
				continue
			}
			fmt.Printf("aios serve: claiming issue #%d %q\n", iss.Number, iss.Title)
			if err := runner.RunIssue(ctx, iss); err != nil {
				fmt.Fprintf(os.Stderr, "issue #%d: %v\n", iss.Number, err)
			}
			_ = state.Save(statePath)
			break // sequential — handle one per poll cycle
		}
		return nil
	}

	if once {
		return doOne(cmd.Context())
	}
	interval := time.Duration(cfg.Poll.IntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if err := doOne(cmd.Context()); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}
	for {
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-ticker.C:
			if err := doOne(cmd.Context()); err != nil {
				fmt.Fprintf(os.Stderr, "warn: %v\n", err)
			}
		}
	}
}

// subprocessAutopilot is the production AutopilotFn — shells out to
// `aios autopilot "<idea>"` and parses the resulting autopilot-summary.md
// from the latest .aios/runs/<id>/ directory.
func subprocessAutopilot(ctx context.Context, idea string) (AutopilotResult, error) {
	wd, err := os.Getwd()
	if err != nil {
		return AutopilotResult{}, err
	}
	runsDir := filepath.Join(wd, ".aios", "runs")
	beforeIDs := snapshotRunIDs(runsDir)

	cmd := exec.CommandContext(ctx, os.Args[0], "autopilot", idea)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	exitErr := cmd.Run() // capture exit; status is encoded in the summary

	afterIDs := snapshotRunIDs(runsDir)
	newID := newestNew(beforeIDs, afterIDs)
	if newID == "" {
		return AutopilotResult{}, fmt.Errorf("autopilot ran but no new run dir under %s (exit: %v)", runsDir, exitErr)
	}
	summaryPath := filepath.Join(runsDir, newID, "autopilot-summary.md")
	body, err := os.ReadFile(summaryPath)
	if err != nil {
		return AutopilotResult{}, fmt.Errorf("read autopilot-summary.md: %w", err)
	}
	return parseAutopilotSummary(string(body))
}

func snapshotRunIDs(runsDir string) map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir(runsDir)
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}

func newestNew(before, after map[string]bool) string {
	var newest string
	for id := range after {
		if before[id] {
			continue
		}
		if id > newest {
			newest = id
		}
	}
	return newest
}

// parseAutopilotSummary reads the markdown summary file written by run.go's
// writeAutopilotSummary. Looks for: "Skipped: ...", "PR: <url>", "Merged: <bool>".
func parseAutopilotSummary(body string) (AutopilotResult, error) {
	res := AutopilotResult{Status: AutopilotUnknown}
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
			res.Status = AutopilotMerged
		case strings.HasPrefix(ln, "Merged: false"):
			res.Status = AutopilotPRRed
		case strings.Contains(ln, "all tasks abandoned") || strings.Contains(ln, "Skipped: no converged tasks"):
			res.Status = AutopilotAbandoned
			res.AuditTrail = body
		}
	}
	if res.Status == AutopilotUnknown {
		return res, fmt.Errorf("autopilot-summary.md did not yield a recognised status:\n%s", body)
	}
	return res, nil
}
```

- [ ] **Step 3: Register the command in `internal/cli/root.go`**

Find the `AddCommand` block in `NewRootCmd`. Add:

```go
	root.AddCommand(newServeCmd())
```

after the existing `newAutopilotCmd()` registration.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/... -run TestServeCmdHelp -v`
Expected: PASS.

Run: `go test ./...`
Expected: full suite green.

Run: `go build ./... && go vet ./...`
Expected: clean.

Run: `go run ./cmd/aios --help` and verify `serve` shows up in Available Commands.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/serve.go internal/cli/serve_test.go internal/cli/root.go
git commit -m "feat(cli): aios serve poll loop with subprocess autopilot wiring"
```

---

## Task 5: Integration tests for serve mode

**Files:**
- Create: `test/integration/serve_oneshot_test.go`
- Create: `test/integration/serve_recovery_test.go`

End-to-end coverage of the four behavioural axes (merged / abandoned / pr-red / recovery) by driving `ServeRunner` with FakeHost + injected `AutopilotFn`.

- [ ] **Step 1: Create `test/integration/serve_oneshot_test.go`**

```go
package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

func defaultServeConfig() *cli.ServeConfig {
	c := &cli.ServeConfig{}
	cli.ApplyServeDefaultsForTest(c)
	return c
}

func TestServe_Merged_ClosesIssueWithDoneLabel(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "endpoint", Labels: []string{"aios:do"}},
	}}
	state := cli.NewServeState()
	runner := &cli.ServeRunner{
		Host: host, State: state, Config: defaultServeConfig(),
		Autopilot: func(_ context.Context, _ string) (cli.AutopilotResult, error) {
			return cli.AutopilotResult{Status: cli.AutopilotMerged, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if !host.Closed[42] {
		t.Error("merged issue must be closed")
	}
	labels := labelsAsSet(host.Issues, 42)
	if !labels["aios:done"] {
		t.Errorf("aios:done expected, got %v", labels)
	}
	if labels["aios:do"] || labels["aios:in-progress"] {
		t.Errorf("aios:do and aios:in-progress should be removed, got %v", labels)
	}
}

func TestServe_Abandoned_FilesStuckIssue(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Refactor everything", Body: "vague request", Labels: []string{"aios:do"}},
	}}
	state := cli.NewServeState()
	runner := &cli.ServeRunner{
		Host: host, State: state, Config: defaultServeConfig(),
		Autopilot: func(_ context.Context, _ string) (cli.AutopilotResult, error) {
			return cli.AutopilotResult{Status: cli.AutopilotAbandoned, AuditTrail: "stall_no_progress: ..."}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	if len(host.OpenedIssues) != 1 {
		t.Fatalf("expected 1 stuck issue, got %d", len(host.OpenedIssues))
	}
	stuck := host.OpenedIssues[0]
	if !strings.Contains(stuck.Title, "[aios:stuck]") {
		t.Errorf("stuck title = %q, want [aios:stuck] prefix", stuck.Title)
	}
	if host.Closed[42] {
		t.Error("abandoned issue must NOT be closed (waiting for human triage)")
	}
}

func TestServe_PROpenRed_KeepsIssueOpen(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "Add /health", Body: "", Labels: []string{"aios:do"}},
	}}
	state := cli.NewServeState()
	runner := &cli.ServeRunner{
		Host: host, State: state, Config: defaultServeConfig(),
		Autopilot: func(_ context.Context, _ string) (cli.AutopilotResult, error) {
			return cli.AutopilotResult{Status: cli.AutopilotPRRed, PRNumber: 99, PRURL: "https://example.invalid/pull/99"}, nil
		},
	}
	if err := runner.RunIssue(context.Background(), host.Issues[0]); err != nil {
		t.Fatalf("RunIssue: %v", err)
	}
	labels := labelsAsSet(host.Issues, 42)
	if !labels["aios:pr-open"] {
		t.Errorf("aios:pr-open expected, got %v", labels)
	}
	if host.Closed[42] {
		t.Error("CI-red issue must not be closed")
	}
	if len(host.Comments[42]) == 0 || !strings.Contains(host.Comments[42][0], "#99") {
		t.Errorf("expected comment referencing PR #99, got %v", host.Comments[42])
	}
}

func labelsAsSet(issues []githost.Issue, num int) map[string]bool {
	m := map[string]bool{}
	for _, i := range issues {
		if i.Number == num {
			for _, l := range i.Labels {
				m[l] = true
			}
		}
	}
	return m
}
```

The `ApplyServeDefaultsForTest` helper needs to be exported. Add a small `internal/cli/export_for_tests.go` extension:

```go
// ApplyServeDefaultsForTest exposes applyServeDefaults to integration tests.
func ApplyServeDefaultsForTest(c *ServeConfig) { applyServeDefaults(c) }
```

(Either append to the existing `export_for_tests.go` from M1 or create a new one — your call. Match what's already there.)

- [ ] **Step 2: Create `test/integration/serve_recovery_test.go`**

```go
package integration

import (
	"context"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
	"github.com/MoonCodeMaster/AIOS/internal/githost"
)

// TestServe_Recovery_GitHubOrphanReleased simulates a killed daemon: GitHub
// shows an issue with aios:in-progress, but local state.json is empty.
// Reconcile must release the orphan back to aios:do.
func TestServe_Recovery_GitHubOrphanReleased(t *testing.T) {
	host := &githost.FakeHost{Issues: []githost.Issue{
		{Number: 42, Title: "in-flight before kill", Labels: []string{"aios:in-progress"}},
	}}
	state := cli.NewServeState()
	if err := state.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	labels := map[string]bool{}
	for _, l := range host.Issues[0].Labels {
		labels[l] = true
	}
	if labels["aios:in-progress"] {
		t.Error("aios:in-progress should be removed by reconcile")
	}
	if !labels["aios:do"] {
		t.Errorf("aios:do should be re-added by reconcile, got %v", labels)
	}
}

// TestServe_Recovery_StateOrphanDropped simulates the inverse drift: state.json
// thinks an issue is in-flight but GitHub has no aios:in-progress label.
// Reconcile must drop the entry from state.
func TestServe_Recovery_StateOrphanDropped(t *testing.T) {
	host := &githost.FakeHost{} // no issues at all
	state := cli.NewServeState()
	state.Add(99, "stale-run")
	if err := state.Reconcile(context.Background(), host, "aios:do", "aios:in-progress"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, present := state.Issues[99]; present {
		t.Errorf("state-only orphan should be dropped, state = %+v", state.Issues)
	}
}
```

- [ ] **Step 3: Run integration tests**

Run: `go test ./test/integration/... -run TestServe_ -v`
Expected: PASS — five tests.

Run: `go test ./...`
Expected: full suite green.

- [ ] **Step 4: Commit**

```bash
git add test/integration/serve_oneshot_test.go test/integration/serve_recovery_test.go internal/cli/export_for_tests.go
git commit -m "test(integration): aios serve label state machine and recovery"
```

---

## Task 6: Docs — README + architecture

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`

- [ ] **Step 1: Update README.md**

Find the `## Autopilot mode (no human input)` section. After it (and after the "Auto-decompose" subsection if present), add:

```markdown
## Serve mode (issue bot)

`aios serve` watches a GitHub repo for issues labeled `aios:do` and runs
autopilot for each one. The bot opens the PR, comments back on the issue with
the PR link, closes the issue on merge, and files an `aios:stuck` issue with
the audit trail when autopilot abandons.

```bash
gh auth login                                # one-time
aios serve --repo MoonCodeMaster/AIOS        # daemon
aios serve --repo MoonCodeMaster/AIOS --once # single poll, for cron
```

Configure via `.aios/serve.toml` (all fields optional; defaults shown):

```toml
[repo]
owner = ""    # falls back to current git remote
name = ""

[labels]
do          = "aios:do"
in_progress = "aios:in-progress"
pr_open     = "aios:pr-open"
stuck       = "aios:stuck"
done        = "aios:done"

[poll]
interval_sec = 60

[concurrency]
max_concurrent_issues = 1   # clamped to 1 in v0.5.0
```

State persists at `.aios/serve/state.json`. A killed daemon reconciles on
restart: `aios:in-progress` issues with no local state are released back to
`aios:do` for retry.
```

Also update the Project status known-limitations list. Replace the existing line about serve mode (if any) or add this note:

```markdown
- `aios serve` ships sequential-only in v0.5.0. The `[concurrency]
  max_concurrent_issues` config knob exists but is clamped to 1 internally
  pending per-issue `.aios/` workspace isolation.
```

Run:

```bash
grep -nE "comprehensive|robust|leverage|facilitate|ensure that|🤖|Generated by" README.md || echo "clean"
```

Expected: `clean`.

- [ ] **Step 2: Update docs/architecture.md**

Add a new `### Serve mode (`internal/cli/serve.go`)` subsection between the existing "Auto-decompose" and "Data on disk" sections:

```markdown
### Serve mode (`internal/cli/serve.go`)

`aios serve` is a poll-driven daemon that watches a GitHub repo for issues
labeled `aios:do`. Per cycle:

1. `ListLabeled("aios:do")` via the existing `gh` adapter (extended in M4).
2. For each issue not already tracked in `.aios/serve/state.json`:
   - Move label: `aios:do` → `aios:in-progress`. Save state.
   - Render idea string = title + body, verbatim.
   - Subprocess `aios autopilot "<idea>"`. Parse `autopilot-summary.md` of
     the resulting run directory.
   - Match outcome: `merged` → comment + close + `aios:done`; `pr-red` →
     comment + `aios:pr-open` (issue stays open); `abandoned` → open
     `[aios:stuck]` issue with audit trail + comment + `aios:stuck`.
   - Clear state entry.

Crash safety: `.aios/serve/state.json` records every claim. On startup,
`Reconcile` resolves drift between GitHub labels and local state by walking
the symmetric difference — GitHub-only orphans go back to `aios:do`,
state-only orphans are dropped from the file.

v0.5.0 ships sequential (one issue per poll). Concurrent execution requires
per-issue `.aios/` workspace isolation, which is deferred.
```

Run:

```bash
grep -nE "comprehensive|robust|leverage|facilitate|ensure that|🤖|Generated by" docs/architecture.md || echo "clean"
```

Expected: `clean`.

- [ ] **Step 3: Commit**

```bash
git add README.md docs/architecture.md
git commit -m "docs: aios serve mode in README and architecture"
```

---

## Self-review checklist

- [ ] **Spec coverage:** Issue ops on Host (Task 1). Serve config + state (Task 2). Per-issue runner with label state machine (Task 3). `aios serve` command + `--once` (Task 4). Crash recovery via reconcile (Tasks 2, 5). Integration tests for all outcomes + recovery (Task 5). Docs (Task 6). Concurrency clamp documented as v0.5.0 scope cut.
- [ ] **TDD discipline:** Tasks 1–5 are TDD; Task 6 is docs.
- [ ] **No placeholders:** Every step has complete code or precise commands.
- [ ] **Type consistency:** `Host` issue methods, `ServeConfig`, `ServeState`, `ServeRunner`, `AutopilotResult`, `AutopilotStatus` all referenced consistently across tasks.
- [ ] **Frequent commits:** One commit per task. Lowercase conventional prefixes. No `Co-Authored-By` or `🤖 Generated`.
- [ ] **Build green at every commit:** Each task ends with `go test ./...` clean.
- [ ] **Existing behaviour preserved:** `aios serve` is a new command; nothing else changes. The `Host` interface gains methods but existing callers (autopilot finalizer) only use the original three.

---

## Out of scope (deferred)

- **True concurrency >1** — needs per-issue `.aios/` workspace isolation. Tracked as M4 follow-up.
- **Multi-repo serve** — single `[repo]` per serve invocation. Multi-repo would require routing logic in the runner and is premature for v0.5.
- **Discussions tab integration** — manual GitHub setting; M3 added the link in `config.yml`.
- **Webhook-driven serve** (instead of polling) — needs a public listener (ngrok/cloudflared) which is a deployment story, not a code one.
- **Metrics / observability** — Prometheus endpoint, /healthz, etc.

---

## Done criteria

- All 6 tasks merged.
- `go test ./...` green.
- A manual smoke run: open a real issue with label `aios:do` on the AIOS repo, run `aios serve --once --repo MoonCodeMaster/AIOS`, and verify the bot transitions the labels and either merges a PR or files an `aios:stuck` issue.
- Tag `v0.5.0` cut.
