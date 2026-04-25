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
