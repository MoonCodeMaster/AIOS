# M3 — Hardening Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface MCP call failures into the reviewer's prompt; persist per-round MCP call records to disk (close the README claim that this already happens); add `.github/` issue + PR templates; write `docs/architecture.md`. Targets release **v0.3.1**.

**Architecture:** No architecture changes. The orchestrator's `RoundRecord` grows an `McpCalls` field captured from the coder's `InvokeResponse`; the reviewer-render signature gains a `mcpFailures` argument; `reviewer.tmpl` renders a new conditional section. The CLI's per-round artefact loop adds one extra `mcp-calls.json` write. Documentation and `.github/` files are additive.

**Tech Stack:** Go 1.26.2, existing template/text-template stack, GitHub form schema (YAML) for issue templates.

**Spec reference:** `docs/superpowers/specs/2026-04-25-autopilot-roadmap-design.md` § M3.

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `.github/ISSUE_TEMPLATE/bug.yml` | GitHub form for bug reports — title, repro steps, expected/actual, version. |
| `.github/ISSUE_TEMPLATE/feature.yml` | Feature request form — problem, proposed solution, alternatives. |
| `.github/ISSUE_TEMPLATE/question.yml` | Question form, routes the firehose away from bugs/features. |
| `.github/PULL_REQUEST_TEMPLATE.md` | PR description scaffold — what changed, why, test plan. |
| `docs/architecture.md` | One-page architecture overview: orchestrator state machine, merge queue, autopilot finalizer, decompose pipeline. |

**Modified files:**

| Path | Change |
|---|---|
| `internal/orchestrator/orchestrator.go` | Add `McpCalls []engine.McpCall` to `RoundRecord`. Store `cres.McpCalls` after coder invoke. Compute `mcpFailures` (calls where `Error != ""` or `Denied`) and pass them into `Deps.RenderReviewer`. |
| `internal/orchestrator/orchestrator.go` (Deps) | Extend `Deps.RenderReviewer` signature: add `mcpFailures []engine.McpCall` parameter. |
| `internal/orchestrator/orchestrator_test.go` | Update default-render shim if needed; add a test that asserts MCP failures from a coder response surface in the next reviewer prompt. |
| `internal/cli/run.go` | Update `renderReviewer` closure and `reviewerData` struct to receive MCP failures and forward them to the template. Add `mcp-calls.json` write to the per-round artefact loop. |
| `internal/engine/prompts/reviewer.tmpl` | Add `## MCP failures this round` section (rendered only when failures exist). |
| `internal/engine/prompts/prompts_test.go` | Add a test that exercises the MCP-failure branch of the reviewer template. |
| `README.md` | Remove the "MCP failures not surfaced inside reviewer" caveat from Project status. |

---

## Implementation order

1. Tasks 1–2: surface MCP failures (RoundRecord field, render signature, template).
2. Task 3: persist `mcp-calls.json` per round.
3. Task 4: `.github/` issue + PR templates (mechanical).
4. Task 5: `docs/architecture.md`.
5. Task 6: README polish.

---

## Task 1: Capture MCP calls per round and surface failures to the reviewer

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`
- Modify: `internal/orchestrator/orchestrator_test.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/engine/prompts/reviewer.tmpl`
- Modify: `internal/engine/prompts/prompts_test.go`

The coder's `InvokeResponse.McpCalls` already exists (engines populate it). This task threads it into `RoundRecord`, into the reviewer prompt as a new `MCPFailures` template variable, and renders a section in `reviewer.tmpl` when any failure is present.

- [ ] **Step 1: Write the failing template-render test**

Append to `internal/engine/prompts/prompts_test.go`:

```go
func TestRender_Reviewer_IncludesMCPFailures(t *testing.T) {
	out, err := Render("reviewer.tmpl", map[string]any{
		"Task":  map[string]any{"ID": "001", "Kind": "feature", "Acceptance": []string{"c1"}},
		"Diff":  "diff content",
		"Checks": []map[string]any{{"Name": "test_cmd", "Status": "passed"}},
		"MCPFailures": []map[string]any{
			{"Server": "github", "Tool": "search_code", "Error": "401 unauthorized"},
			{"Server": "docs", "Tool": "fetch", "Error": "timeout"},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"MCP failures", "github", "search_code", "401 unauthorized", "docs", "timeout"} {
		if !strings.Contains(out, want) {
			t.Errorf("reviewer.tmpl with MCPFailures missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_Reviewer_OmitsMCPSectionWhenNoFailures(t *testing.T) {
	out, err := Render("reviewer.tmpl", map[string]any{
		"Task":   map[string]any{"ID": "001", "Kind": "feature", "Acceptance": []string{"c1"}},
		"Diff":   "diff content",
		"Checks": []map[string]any{{"Name": "test_cmd", "Status": "passed"}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "MCP failures") {
		t.Errorf("reviewer.tmpl with no MCPFailures must not render the MCP section\n--- output ---\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests — should fail**

Run: `go test ./internal/engine/prompts/... -run TestRender_Reviewer -v`
Expected: FAIL on the new test (template doesn't yet have the MCP section).

- [ ] **Step 3: Update `reviewer.tmpl`**

Edit `internal/engine/prompts/reviewer.tmpl`. Insert this block after the existing `# Verification results` section and BEFORE the `# Diff (against staging)` section:

```
{{- if .MCPFailures}}

# MCP failures this round

The coder attempted these MCP tool calls and they failed. When evaluating
acceptance criteria, distinguish "the coder ignored a requirement" from
"the coder couldn't reach external context."

{{- range .MCPFailures}}
- {{.Server}}.{{.Tool}}: {{.Error}}
{{- end}}
{{- end}}
```

The `{{- if .MCPFailures}}...{{- end}}` guards the section so it disappears when there are no failures.

- [ ] **Step 4: Run template tests — should pass**

Run: `go test ./internal/engine/prompts/... -v`
Expected: both new tests PASS; existing tests still PASS.

- [ ] **Step 5: Add `McpCalls` to `RoundRecord`**

Edit `internal/orchestrator/orchestrator.go`. Find the `RoundRecord` struct and add `McpCalls`:

```go
type RoundRecord struct {
	N           int
	CoderPrompt string
	CoderText   string
	CoderRaw    string
	Escalated   bool
	ReviewerPrompt string
	ReviewerRaw    string
	Review         ReviewResult
	Checks         []verify.CheckResult
	UsageTokens    int
	// McpCalls captures every MCP tool call the coder made this round
	// (success and failure alike). Failed calls — Error != "" or Denied — are
	// surfaced into the reviewer prompt so the reviewer can distinguish a
	// coder mistake from an MCP outage.
	McpCalls []engine.McpCall
}
```

- [ ] **Step 6: Extend `Deps.RenderReviewer` signature**

In the same file, find the `Deps` struct and update `RenderReviewer`:

```go
RenderReviewer func(task *spec.Task, diff string, checks []verify.CheckResult, mcpFailures []engine.McpCall) string
```

Then update the orchestrator's reviewer-render call site (around the existing `rp := d.RenderReviewer(task, diff, r.Checks)` line). Replace with:

```go
		// Capture MCP calls from the coder's response so the audit trail and
		// the reviewer prompt both see them.
		r.McpCalls = cres.McpCalls
		var mcpFailures []engine.McpCall
		for _, m := range cres.McpCalls {
			if m.Error != "" || m.Denied {
				mcpFailures = append(mcpFailures, m)
			}
		}
		// --- reviewing ---
		diff, _ := d.Diff()
		rp := d.RenderReviewer(task, diff, r.Checks, mcpFailures)
```

(Replace the entire chunk from `// --- reviewing ---` through `rp := d.RenderReviewer(...)`.)

Also update the package-private `defaultReviewerRender` helper (used as a fallback in `Deps`):

```go
func defaultReviewerRender(task *spec.Task, diff string, checks []verify.CheckResult, mcpFailures []engine.McpCall) string {
	return fmt.Sprintf("Review task %s. Diff:\n%s\nChecks: %+v\nAcceptance: %v",
		task.ID, diff, checks, task.Acceptance)
}
```

The existing tests pass `RenderReviewer` closures that match the old signature. Update every test caller in `internal/orchestrator/orchestrator_test.go` (and any other test files that build a `*Deps`) to add the new fourth parameter (it can be ignored: `_ = mcpFailures`).

- [ ] **Step 7: Update the CLI's reviewer render closure**

Edit `internal/cli/run.go`. Find the `reviewerData` struct (used by `renderReviewer`) and add `MCPFailures`:

```go
type reviewerData struct {
	Project       *spec.Project
	Task          *spec.Task
	Diff          string
	Checks        []verify.CheckResult
	MCPFailures   []engine.McpCall
}
```

Then update the `renderReviewer` closure:

```go
		renderReviewer := func(task *spec.Task, diff string, ck []verify.CheckResult, mcpFailures []engine.McpCall) string {
			out, err := prompts.Render("reviewer.tmpl", reviewerData{
				Project:     pctx.Project,
				Task:        task,
				Diff:        diff,
				Checks:      ck,
				MCPFailures: mcpFailures,
			})
			if err != nil {
				return fmt.Sprintf("reviewer render error: %v\nTask: %s\nDiff:\n%s",
					err, task.ID, diff)
			}
			return out
		}
```

If there's a `reReview` closure further down the file (used by the merge queue), it also calls `prompts.Render("reviewer.tmpl", reviewerData{...})`. Update its `reviewerData` literal to include `MCPFailures: nil` (rebase re-review doesn't have access to round MCP calls; nil renders cleanly because of the `{{- if .MCPFailures}}` guard).

- [ ] **Step 8: Add a unit test for the orchestrator's MCP-failure surfacing**

Append to `internal/orchestrator/orchestrator_test.go`:

```go
func TestRun_RoundRecordCapturesMcpCalls(t *testing.T) {
	approve := `{"approved":true,"criteria":[{"id":"c1","status":"satisfied"}],"issues":[]}`
	coder := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "coded", McpCalls: []engine.McpCall{
			{Server: "github", Tool: "search_code", Error: "401"},
			{Server: "docs", Tool: "fetch"}, // success
		}},
	}}
	reviewer := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{{Text: approve}}}

	var capturedFailures []engine.McpCall
	dep := &Deps{
		Coder: coder, Reviewer: reviewer,
		Verifier: stubVerifier{[]verify.CheckResult{{Name: "test_cmd", Status: verify.StatusPassed}}},
		RenderReviewer: func(_ *spec.Task, _ string, _ []verify.CheckResult, mcpFailures []engine.McpCall) string {
			capturedFailures = mcpFailures
			return "review prompt"
		},
		MaxRounds: 5, MaxTokens: 10000, MaxWall: time.Minute,
	}
	task := &spec.Task{ID: "001", Kind: "feature", Status: "pending", Acceptance: []string{"c1"}}
	out, err := Run(context.Background(), task, dep)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != StateConverged {
		t.Fatalf("final = %s", out.Final)
	}
	if len(out.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1", len(out.Rounds))
	}
	if len(out.Rounds[0].McpCalls) != 2 {
		t.Errorf("RoundRecord.McpCalls = %d, want 2 (full call list)", len(out.Rounds[0].McpCalls))
	}
	if len(capturedFailures) != 1 || capturedFailures[0].Server != "github" {
		t.Errorf("reviewer received MCP failures = %+v, want exactly the failed github.search_code call", capturedFailures)
	}
}
```

The test uses `stubVerifier` from existing tests in the package; if the package's test file doesn't have one yet, define it inline:

```go
type stubVerifier struct{ r []verify.CheckResult }
func (s stubVerifier) Run() []verify.CheckResult { return s.r }
```

Add `time` and any other missing imports to the test file.

- [ ] **Step 9: Verify**

Run: `go test ./internal/orchestrator/... -run TestRun_RoundRecordCapturesMcpCalls -v`
Expected: PASS.

Run: `go test ./...`
Expected: full suite green. (Compile errors will surface every place that builds a `*Deps` with the old `RenderReviewer` signature — fix each by adding `_` for the new `mcpFailures` parameter.)

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 10: Commit**

```bash
git add internal/orchestrator/orchestrator.go internal/orchestrator/orchestrator_test.go internal/cli/run.go internal/engine/prompts/reviewer.tmpl internal/engine/prompts/prompts_test.go
git commit -m "feat(reviewer): surface MCP call failures in the reviewer prompt"
```

---

## Task 2: Persist `mcp-calls.json` per round

**Files:**
- Modify: `internal/cli/run.go`

The README and the per-round artefact directory layout claim `mcp-calls.json` is written. It isn't. Now that `RoundRecord.McpCalls` exists (Task 1), add the write.

- [ ] **Step 1: Find the per-round artefact loop**

In `internal/cli/run.go`, locate the loop that writes `coder.prompt.txt`, `coder.response.raw`, `verify.json`, etc., for each round. It looks like:

```go
for i, r := range outcome.Rounds {
	_ = rec.WriteRoundFile(tk.ID, i+1, "coder.prompt.txt", []byte(r.CoderPrompt))
	_ = rec.WriteRoundFile(tk.ID, i+1, "coder.response.raw", []byte(r.CoderRaw))
	// ... more writes ...
}
```

- [ ] **Step 2: Add the `mcp-calls.json` write**

After the existing `verify.json` write inside the loop, insert:

```go
		if len(r.McpCalls) > 0 {
			mcpJSON, _ := json.MarshalIndent(r.McpCalls, "", "  ")
			_ = rec.WriteRoundFile(tk.ID, i+1, "mcp-calls.json", mcpJSON)
		}
```

The `len > 0` guard avoids littering empty files for rounds that didn't touch MCP.

- [ ] **Step 3: Verify**

Run: `go test ./...`
Expected: full suite green.

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/run.go
git commit -m "feat(run): persist mcp-calls.json per round when MCP was invoked"
```

---

## Task 3: `.github/` issue and PR templates

**Files:**
- Create: `.github/ISSUE_TEMPLATE/bug.yml`
- Create: `.github/ISSUE_TEMPLATE/feature.yml`
- Create: `.github/ISSUE_TEMPLATE/question.yml`
- Create: `.github/ISSUE_TEMPLATE/config.yml` (disables blank issues, links to Discussions if enabled)
- Create: `.github/PULL_REQUEST_TEMPLATE.md`

GitHub renders these automatically. No code; pure repo config.

- [ ] **Step 1: Create `.github/ISSUE_TEMPLATE/bug.yml`**

```yaml
name: Bug report
description: Something in AIOS is broken or behaves incorrectly.
title: "[bug] "
labels: ["bug"]
body:
  - type: textarea
    id: what-happened
    attributes:
      label: What happened?
      description: A clear description of the bug. What did you expect? What did you see instead?
      placeholder: |
        I ran `aios autopilot "..."` and ...
        I expected ...
        Instead, I saw ...
    validations:
      required: true
  - type: textarea
    id: repro
    attributes:
      label: Steps to reproduce
      placeholder: |
        1. ...
        2. ...
        3. ...
    validations:
      required: true
  - type: input
    id: version
    attributes:
      label: AIOS version
      description: Output of `aios --version`.
      placeholder: "v0.3.0"
    validations:
      required: true
  - type: textarea
    id: environment
    attributes:
      label: Environment
      description: OS, Go version (if building from source), Claude/Codex CLI versions.
      placeholder: |
        macOS 14.4
        go version go1.26.2
        claude --version
        codex --version
  - type: textarea
    id: logs
    attributes:
      label: Relevant logs / audit trail
      description: Output of the failing run, or the contents of `.aios/runs/<run>/<task>/round-N/`.
      render: shell
```

- [ ] **Step 2: Create `.github/ISSUE_TEMPLATE/feature.yml`**

```yaml
name: Feature request
description: Suggest an improvement or new capability.
title: "[feature] "
labels: ["enhancement"]
body:
  - type: textarea
    id: problem
    attributes:
      label: Problem
      description: What can't you do today, or what's painful?
      placeholder: When I ..., AIOS does ..., which means I have to ...
    validations:
      required: true
  - type: textarea
    id: proposal
    attributes:
      label: Proposed solution
      description: What would you like AIOS to do?
    validations:
      required: true
  - type: textarea
    id: alternatives
    attributes:
      label: Alternatives considered
      description: What else did you think about? Why is the proposed solution better?
```

- [ ] **Step 3: Create `.github/ISSUE_TEMPLATE/question.yml`**

```yaml
name: Question
description: Ask a question about AIOS — usage, design choices, integration.
title: "[question] "
labels: ["question"]
body:
  - type: textarea
    id: question
    attributes:
      label: Question
    validations:
      required: true
  - type: textarea
    id: context
    attributes:
      label: Context
      description: What are you trying to do? What have you already tried?
```

- [ ] **Step 4: Create `.github/ISSUE_TEMPLATE/config.yml`**

```yaml
blank_issues_enabled: false
contact_links:
  - name: Documentation
    url: https://github.com/MoonCodeMaster/AIOS#readme
    about: Start here — README covers install, quick start, autopilot mode.
  - name: Architecture
    url: https://github.com/MoonCodeMaster/AIOS/blob/main/docs/architecture.md
    about: How AIOS works internally — orchestrator, merge queue, decompose.
```

- [ ] **Step 5: Create `.github/PULL_REQUEST_TEMPLATE.md`**

```markdown
## What

<!-- One or two sentences on the change. -->

## Why

<!-- The problem this solves or the use case it enables. Skip if obvious from the title. -->

## Test plan

- [ ] `go test ./...` green locally
- [ ] `go vet ./...` clean
- [ ] <task-specific checks: e.g. ran `aios autopilot` against a toy repo>

## Notes

<!-- Anything reviewers should be aware of: scope decisions, follow-ups, known limitations. -->
```

- [ ] **Step 6: Verify the files**

Run: `ls -la .github/ISSUE_TEMPLATE/ .github/PULL_REQUEST_TEMPLATE.md`
Expected: all five files present.

`go build ./...` and `go test ./...` are unaffected (these are pure repo config).

- [ ] **Step 7: Commit**

```bash
git add .github/ISSUE_TEMPLATE/ .github/PULL_REQUEST_TEMPLATE.md
git commit -m "chore(github): issue and pull request templates"
```

---

## Task 4: `docs/architecture.md`

**Files:**
- Create: `docs/architecture.md`

One page describing how AIOS works internally. Match README tone — terse, declarative, plain English.

- [ ] **Step 1: Create `docs/architecture.md`**

```markdown
# AIOS architecture

A 1500-foot view of how `aios autopilot "<idea>"` actually runs.

## Pipeline

```
your idea
   │
   ▼
aios new --auto ──► brainstorm ──► spec ──► task DAG (one .md per task)
                                              │
                                              ▼
                            dependency-ordered worker pool
                                              │
            ┌─────────────────────────────────┴─────────────────────────────────┐
            ▼                                                                   ▼
    task A (worktree)                                                  task B (worktree)
    coder → verify → reviewer → revise → ... → converged?              (same loop)
                                              │
                                              ▼
                       merge queue (rebase + re-verify + re-review)
                                              │
                                              ▼
                                       aios/staging
                                              │
                                              ▼
                              gh pr create → gh pr checks → gh pr merge
                                              │
                                              ▼
                                            main
```

## Components

### Orchestrator (`internal/orchestrator`)

Runs the per-task state machine. `Run(ctx, task, deps)` loops over rounds:

1. Render coder prompt; invoke coder.
2. Run verify (`go test`, `go vet`, etc.).
3. Render reviewer prompt; invoke reviewer.
4. If reviewer approves AND verify is green AND every acceptance criterion is satisfied → `StateConverged`.
5. Otherwise → next round, with the prior diff and reviewer issues fed back into the coder prompt.

Stall detection: if N consecutive rounds raise the same fingerprint of unmet criteria + issues, the orchestrator either (a) issues a hard-constraint retry round (escalation) or (b) blocks with `CodeStallNoProgress`.

Per-round audit: every prompt, raw response, verify result, parsed reviewer verdict, and MCP call list is persisted under `.aios/runs/<run-id>/<task-id>/round-N/`.

### Scheduler (`internal/orchestrator/scheduler.go`)

DAG bookkeeper. Workers pull task IDs from `Ready()`, complete them via the orchestrator's `Run`, and call `Done(result)`. The scheduler:

- Releases dependents when a task converges.
- Cascades upstream-blocked status when a task blocks.
- Splices children when a task is `decomposed` (M2 auto-decompose) — atomic insertion under the existing mutex; dependents rewire to wait on all children.

### Merge queue (`internal/orchestrator/mergeq.go`)

Serialises the integration step. Each converged task submits a `MergeRequest`; the queue rebases the task branch onto current `aios/staging`, re-runs verify, and (if the rebase changed the diff) re-invokes the reviewer. Only when all three signals are green does the branch fast-forward into `aios/staging`.

### Engines (`internal/engine`)

Thin adapters around the Claude and Codex CLIs. Each engine reads its CLI's JSON output, extracts the assistant text and any MCP tool calls, and returns an `InvokeResponse`. The orchestrator never touches the CLI directly.

Cross-model pairing is enforced at config load and at runtime: a single AIOS run cannot use the same engine for both coder and reviewer.

### MCP (`internal/mcp`)

Per-task MCP scope. Each task declares `mcp_allow: [...]` in its frontmatter; the manager intersects the task's allowlist with the run-wide `[mcp.servers]` config and renders a per-task MCP config file the engine reads. Failed MCP calls (transport error, denied by allowlist) are surfaced into the reviewer prompt so the reviewer can distinguish "coder ignored a constraint" from "coder couldn't reach external context."

### Worktrees (`internal/worktree`)

Every task runs in its own `git worktree` on `aios/task/<id>`. Branches are preserved across runs (so `git log aios/task/<id>` retains history), but worktrees are GC'd at startup if a previous run crashed before cleanup. Resumed tasks reuse the existing branch — `Create` checks for the ref and attaches without `-b` when it exists.

### Autopilot (`internal/cli/autopilot.go`, `internal/cli/run.go`)

`aios autopilot "<idea>"` is a thin wrapper that runs `aios new --auto` then `aios run --autopilot --merge`. The autopilot finalizer:

1. Preflights `gh` on PATH and `gh auth status`.
2. After `RunAll` returns, partitions blocked tasks into "real blocks" and "autopilot abandons" (the latter being autopilot's "drop and continue" path).
3. If real blocks exist → `os.Exit(2)` with a block summary.
4. Otherwise → opens a PR via `gh pr create`, polls `gh pr checks` until green/red/timeout, and squash-merges on green.

### Auto-decompose (`internal/cli/decompose`)

When an autopilot task stalls AND `task.Depth < cfg.Budget.DecomposeDepthCap()`:

1. Claude and Codex propose splits in parallel via `decompose-stuck.tmpl`.
2. Whichever engine reviewed the stuck task synthesises both proposals via `decompose-merge.tmpl`.
3. Sub-tasks are stamped `<parent>.<n>`, written to `.aios/tasks/`, and spliced into the live scheduler. The parent's frontmatter is marked `decomposed`.
4. Fallbacks: one proposal errors → use the survivor; both error → ErrAbandon; synthesizer errors → deterministic union dedupe with synthesizer-side tiebreak.

## Data on disk

```
.aios/
├── config.toml                  # run config (engines, budget, verify, MCP)
├── project.md                   # synthesised spec from `aios new`
├── tasks/                       # one .md per task (frontmatter + body)
│   ├── 001-foo.md
│   └── 005.1.md                 # decomposed sub-tasks live here too
├── worktrees/                   # per-task git worktrees (GC'd at startup)
│   └── 001-foo/
└── runs/<run-id>/               # per-run audit
    ├── brainstorm.md
    ├── autopilot-summary.md
    ├── abandoned/<task>/        # full audit trail for autopilot-dropped tasks
    │   ├── report.md
    │   └── full-trail.json
    └── <task>/round-N/
        ├── coder.prompt.txt
        ├── coder.response.raw
        ├── coder-text.txt
        ├── verify.json
        ├── reviewer.prompt.txt
        ├── reviewer.response.raw
        ├── reviewer-response.json
        └── mcp-calls.json       # only when MCP was invoked
```

## Process boundaries

AIOS is a single Go process per run. The Claude and Codex CLIs are separate child processes invoked synchronously per coder/reviewer call. MCP servers are long-lived child processes managed by `internal/mcp.Manager` and shut down cleanly on run completion.

GitHub interaction goes through the `gh` CLI as another child process — no native Go GitHub client.
```

- [ ] **Step 2: Verify**

Run: `head -1 docs/architecture.md`
Expected: `# AIOS architecture`

Run: `grep -nE "comprehensive|robust|leverage|facilitate|🤖|Generated by" docs/architecture.md || echo "clean"`
Expected: `clean`.

- [ ] **Step 3: Commit**

```bash
git add docs/architecture.md
git commit -m "docs(architecture): one-page system overview"
```

---

## Task 5: README polish — remove obsolete caveat

**Files:**
- Modify: `README.md`

Now that MCP failures land in the reviewer prompt (Task 1), remove that caveat from the Project status known-limitations list.

- [ ] **Step 1: Find and update the bullet**

In `README.md`'s `## Project status` section, find the bullet that reads:

```
- MCP call failures are surfaced in audit logs; surfacing them inside the
  reviewer prompt is shipping in v0.3.1.
```

Replace with:

```
- MCP call failures are surfaced both in the per-round audit
  (`mcp-calls.json`) and inline in the reviewer prompt — the reviewer can
  distinguish "coder ignored a constraint" from "coder couldn't reach
  external context."
```

Also link the new architecture doc near the top of the file. Find a sensible spot (e.g. after the "Pipeline" section or in the "Why AIOS exists" / first-paragraph area) and add:

```markdown
For an internal tour, see [`docs/architecture.md`](docs/architecture.md).
```

A single line, placed where it naturally fits — pick the spot.

- [ ] **Step 2: Forbidden-language check**

Run:

```bash
grep -nE "comprehensive|robust|leverage|facilitate|ensure that|🤖|Generated by" README.md || echo "clean"
```

Expected: `clean`.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): MCP failures land in reviewer prompt; link architecture"
```

---

## Self-review checklist

- [ ] **Spec coverage:**
  - "MCP-failure → reviewer prompt (P1-4)" → Tasks 1, 2.
  - "Issue + PR templates (P2-8)" → Task 3.
  - "`docs/architecture.md` (P2-7)" → Task 4.
  - Asciicast and `--sandbox` deferral are explicitly out-of-scope per the spec; no tasks needed.
- [ ] **TDD discipline:** Task 1 follows test-first; Tasks 2–5 are mechanical (file additions + small README edit) and don't need separate tests beyond the suite running clean.
- [ ] **No placeholders:** Every step has complete content.
- [ ] **Type consistency:** `RoundRecord.McpCalls`, `Deps.RenderReviewer`'s `mcpFailures` parameter, `reviewerData.MCPFailures`, and the template's `.MCPFailures` variable all match.
- [ ] **Frequent commits:** One commit per task. Lowercase conventional-commit prefixes; no `Co-Authored-By` or `🤖 Generated`.
- [ ] **Build green at every commit:** Each task ends with `go test ./...` clean (or N/A for non-code tasks).
- [ ] **Existing behaviour preserved:** When `r.McpCalls` is empty, the reviewer prompt looks identical to before. The `mcp-calls.json` write is gated on `len > 0`. The `RenderReviewer` signature change is internal — no public API breakage.

---

## Out of scope (deferred)

- **Asciicast in README** — recording requires manual ttygif/asciinema work; defer to whenever the maintainer next does a release demo.
- **Discussions tab** — must be enabled in GitHub repo settings (Settings → General → Features → Discussions). Not a file-level change. Note in release notes; the maintainer enables it manually.
- **`--sandbox` Docker isolation** — explicitly post-1.0 per the roadmap.
- **"Why AIOS" section promotion** — the README already opens with cross-model review framing; further marketing copy would be premature and risks the natural-tone constraint.

---

## Done criteria

- All 5 tasks merged.
- `go test ./...` passes.
- `.github/` templates render correctly when previewed via the GitHub UI (manual visual check).
- A round with at least one failed MCP call (real or fake-engine-driven) shows the failure in `reviewer.prompt.txt`.
- Tag `v0.3.1` cut.
