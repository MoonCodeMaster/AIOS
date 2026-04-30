# AIOS architecture

A 1500-foot view of how `aios --ship "<prompt>"` actually runs.

## Pipeline

```
your prompt
   │
   ▼
specgen (Claude+Codex draft → Codex merge → Claude polish)
   │
   ▼
.aios/project.md ──► decompose ──► task DAG (one .md per task)
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

## Entry points

The bare `aios` binary is the primary surface. Three modes share one pipeline:

- `aios` — interactive REPL (`internal/cli/repl`). Each turn calls
  `specgen.Generate`, writes `.aios/project.md`, persists the session.
  `/ship` inside the REPL hands off to `ShipSpec`.
- `aios "<prompt>"` — one-shot specgen via `runOneShot`. Writes
  `.aios/project.md` and exits. No execution.
- `aios --ship "<prompt>"` — full pipeline via `ShipPrompt`. Specgen, then
  decompose, then the run loop, then PR + merge.
- `aios -p "<prompt>"` — print mode via `runPrintMode`. Polished spec to
  stdout, no side effects.
- `aios --continue [<id>]` — resume a REPL session.

Utility subcommands (`init`, `doctor`, `cost`, `lessons`, `review`, `mcp`,
`status`, `serve`, `run`, `resume`) cover repo bootstrap and observability;
they don't participate in the main pipeline.

## Ship pipeline

`ShipPrompt` (in `internal/cli/spectasks.go`) is the single entry point shared
by `aios --ship`, the REPL `/ship` command, and `aios serve`. It runs:

```
prompt
  │
  ▼
specgen.Generate (4-stage dual-AI) ──► .aios/project.md
  │
  ▼
ShipSpec
  │
  ├── decompose project.md into .aios/tasks/*.md
  ├── runAll (per-task coder↔reviewer loop, merge queue, staging)
  └── on green: gh pr create → gh pr checks → gh pr merge
```

The serve daemon, the REPL, and root `--ship` therefore execute the same
code. There is no subprocess fan-out — `ShipPrompt` runs in-process, which is
why concurrent ship calls in the same working directory would race (today's
`max_concurrent_issues` is clamped to 1 for that reason).

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

Per-task MCP scope. Each task declares `mcp_allow: [...]` in its frontmatter; the manager intersects the task's allowlist with the run-wide `[mcp.servers]` config and renders an engine-scoped MCP config file for the execution coder. Reviewers and auxiliary parallel drafting/review stages do not receive MCP configs; they consume the coder's MCP audit through prompts. Failed MCP calls (transport error, denied by allowlist) are surfaced into the reviewer prompt so the reviewer can distinguish "coder ignored a constraint" from "coder couldn't reach external context."

### Worktrees (`internal/worktree`)

Every task runs in its own `git worktree` on `aios/task/<id>`. Branches are preserved across runs (so `git log aios/task/<id>` retains history), but worktrees are GC'd at startup if a previous run crashed before cleanup. Resumed tasks reuse the existing branch — `Create` checks for the ref and attaches without `-b` when it exists.

### Specgen (`internal/specgen`)

`Generate(ctx, in)` runs the 4-stage pipeline behind every entry point above:

```
        ┌─ Claude draft A ──┐
prompt ─┤                   ├─> Codex merge ──> Claude polish ──> spec
        └─ Codex draft B  ──┘
```

Stages 1 and 2 run in parallel. Intermediate drafts and per-stage timing/token
metrics are persisted under `.aios/runs/<run-id>/specgen/`. Partial failures
(one drafter dead, merge fails, polish fails) degrade gracefully with warnings
on `Output.Warnings`; no automatic retries.

The optional `internal/architect` package — a multi-round mind-map planner —
sits beside specgen and is reserved for Plan 2; it is not wired to any current
CLI command.

### Ship plumbing (`internal/cli/spectasks.go`, `internal/cli/run.go`)

`ShipPrompt` is the in-process entry point. Around it:

1. Preflight `gh` on PATH and `gh auth status`.
2. Run `specgen.Generate`, write `.aios/project.md`.
3. `ShipSpec` decomposes the spec into task files and runs the orchestrator.
4. After `RunAll` returns, partition blocked tasks into "real blocks" and
   "abandons" (the auto-decompose drop-and-continue path).
5. Real blocks → return a structured `ShipResult` with the block summary.
   Otherwise → `gh pr create`, poll `gh pr checks` until green/red/timeout,
   squash-merge on green.

### Auto-decompose (`internal/cli/decompose`)

When a ship-mode task stalls AND `task.Depth < cfg.Budget.DecomposeDepthCap()`:

1. Claude and Codex propose splits in parallel via `decompose-stuck.tmpl`.
2. Whichever engine reviewed the stuck task synthesises both proposals via `decompose-merge.tmpl`.
3. Sub-tasks are stamped `<parent>.<n>`, written to `.aios/tasks/`, and spliced into the live scheduler. The parent's frontmatter is marked `decomposed`.
4. Fallbacks: one proposal errors → use the survivor; both error → ErrAbandon; synthesizer errors → deterministic union dedupe with synthesizer-side tiebreak.

### Serve mode (`internal/cli/serve.go`)

`aios serve` is a poll-driven daemon that watches a GitHub repo for issues
labeled `aios:do`. Per cycle:

1. `ListLabeled("aios:do")` via the existing `gh` adapter.
2. For each issue not already tracked in `.aios/serve/state.json`:
   - Move label: `aios:do` → `aios:in-progress`. Save state.
   - Render prompt = title + body, verbatim.
   - Call `ShipPrompt` in-process. Parse the resulting run summary.
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

## Data on disk

```
.aios/
├── config.toml                  # run config (engines, budget, verify, MCP)
├── project.md                   # synthesised spec from specgen
├── sessions/<id>/session.json   # REPL session state for `aios --continue`
├── tasks/                       # one .md per task (frontmatter + body)
│   ├── 001-foo.md
│   └── 005.1.md                 # decomposed sub-tasks live here too
├── worktrees/                   # per-task git worktrees (GC'd at startup)
│   └── 001-foo/
└── runs/<run-id>/               # per-run audit
    ├── specgen/                 # per-stage drafts + timing/token metrics
    │   ├── 1-claude-draft.md
    │   ├── 2-codex-draft.md
    │   ├── 3-codex-merge.md
    │   └── 4-claude-polish.md
    ├── ship-summary.md
    ├── abandoned/<task>/        # full audit trail for ship-dropped tasks
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

AIOS is a single Go process per run. The Claude and Codex CLIs are separate child processes invoked synchronously per coder/reviewer call, and parallel helper flows use the same process-group cancellation path. Task MCP is coder-only and rendered with engine-specific server names so concurrent reviewer/drafter processes do not spawn duplicate MCP server copies. MCP servers managed by `internal/mcp.Manager` are shut down cleanly on run completion.

GitHub interaction goes through the `gh` CLI as another child process — no native Go GitHub client.

## Interactive entry point and specgen pipeline

`aios` (no subcommand) launches `internal/cli/repl.Repl`, an interactive turn
loop. Each user message is dispatched to `internal/specgen.Generate` (see the
specgen component above). Session state (turn history, current spec path) is
persisted under `.aios/sessions/<id>/session.json` after every turn so a
crashed REPL is resumable via `aios --continue`. The `/ship` slash command
calls the same `ShipSpec` entry point root `--ship` uses, so the spec on disk
and the spec being shipped are guaranteed to match.
