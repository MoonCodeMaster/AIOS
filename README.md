# AIOS

> **Two AIs build your code. A different one checks it. Every prompt, response, and failure is written to disk.**

AIOS drives **Claude CLI** and **Codex CLI** as a coder↔reviewer pair over a
spec-driven task queue. Each task runs in its own `git worktree` on a dedicated
branch. Approved work lands on `aios/staging`. You merge to `main` when you're
ready — the only human step in the loop.

---

## Why AIOS exists

Single-model AI coding loops fail the same way every time: the model that
wrote the code is the one reviewing it, and so misses the exact class of
errors it just introduced. "Claude checks its own PR" and "Codex reviews
its own diff" both converge to the same blind spot.

The only fix that holds up is structural:

- **The engine that writes is not the engine that reviews — ever.** Checked
  at config load *and* at runtime; an AIOS run refuses to start when
  `coder_default == reviewer_default`.
- **Every round's full prompt and raw response is persisted** before the next
  round begins. You can reconstruct exactly what each model saw and said,
  without re-running anything.
- **Each task is physically isolated** in its own `git worktree` on
  `aios/task/<id>`. Parallel tasks cannot contaminate each other, and your
  working checkout is never touched.
- **Verify failures feed the reviewer as blocking issues.** Approved-but-red
  code cannot merge. Stuck loops stop and tell you why — with the reviewer's
  top unresolved issues in the block reason.

## Core advantages

| Advantage | How AIOS does it |
|---|---|
| **Cross-model review (mandatory)** | Config rejects `coder==reviewer`; runtime `engine.PickPair` rechecks. One engine's blind spots get caught by the other. |
| **Full per-round audit trail** | `coder.prompt.txt`, `coder.response.raw`, `reviewer.prompt.txt`, `reviewer.response.raw`, `verify.json`, `reviewer-response.json` persisted per round. |
| **Per-task `git worktree` isolation** | Every task gets `aios/task/<id>` on its own checkout. Startup GC sweeps orphans from crashed prior runs; branches preserved for history. |
| **Verify↔review closed loop** | Red verify is folded into reviewer issues as synthetic blockers. Approval requires all criteria satisfied *and* all checks green. |
| **Structured escalation & stall** | Repeated identical rejections trigger a hard-constraint retry round; if that fails the task blocks with `[NEEDS HUMAN]` and the top reviewer issues in the detail. |
| **Deny-by-default MCP scoping** | Per-task `mcp_allow` intersected with run-wide config. Every MCP call logged to `round-N/mcp-calls.json`. |

## Pipeline

```
   your spec
       │
       ▼
  aios new ──► brainstorm ──► spec ──► task DAG
                                         │
                                         ▼
                                 dependency-ordered worker pool
                                         │
             ┌───────────────────────────┴───────────────────────────┐
             ▼                                                       ▼
     task A (worktree)                                      task B (worktree)
     ┌──────────────────────┐                              ┌──────────────────────┐
     │ coder (Claude)       │                              │ coder (Codex)        │
     │   ↓                  │                              │   ↓                  │
     │ verify (test/lint)   │                              │ verify (test/lint)   │
     │   ↓                  │                              │   ↓                  │
     │ reviewer (Codex)     │                              │ reviewer (Claude)    │
     │   ↓ approved?        │                              │   ↓ approved?        │
     │   no → revise round  │                              │   no → revise round  │
     │   stall → escalate   │                              │   stall → escalate   │
     │   exhausted → BLOCK  │                              │   exhausted → BLOCK  │
     └──────────┬───────────┘                              └──────────┬───────────┘
                │ converged                                           │ converged
                ▼                                                     ▼
                         merge queue (rebase + re-verify + re-review)
                                         │
                                         ▼
                                  aios/staging
                                         │
                                         ▼
                               git merge aios/staging   ← you
                                         │
                                         ▼
                                       main
```

## Install

Prereqs: `git` 2.40+, plus both AI CLIs authenticated:

```bash
npm install -g @anthropic-ai/claude-code    # provides `claude`
npm install -g @openai/codex                # provides `codex`
```

Install AIOS (pick one):

```bash
# Recommended — same ergonomic as claude/codex above.
npm install -g @mooncodemaster/aios

# Alternatives:
brew install MoonCodeMaster/aios/aios              # after first Homebrew tap release
go install github.com/MoonCodeMaster/AIOS/cmd/aios@latest
```

The `npm install` path ships the native `aios` binary. It uses the same
platform-specific `optionalDependencies` pattern as `esbuild`, `biome`,
`swc`, and `@openai/codex`: one tiny launcher plus five tiny sibling
packages, one of which is auto-selected by npm's `os` / `cpu` fields. **No
postinstall scripts. No network download during install.** See
[`docs/npm-distribution.md`](docs/npm-distribution.md) if you need to
troubleshoot `--no-optional`, air-gapped mirrors, or Windows on ARM.

## Quick start

```bash
cd your-repo
aios init                          # writes .aios/config.toml; autodetects Go/Node/Python/Rust
aios new "Add a /health endpoint with a unit test"
# review the proposed spec + task list; confirm with `y`
aios run                           # coder↔reviewer loop until aios/staging is green
git log aios/staging               # audit the coder↔reviewer history
git merge aios/staging             # you're the last human in the loop
```

## Autopilot mode (no human input)

For end-to-end runs with no prompts and no manual `git merge`:

```bash
cd your-repo
aios init
aios autopilot "Add a /health endpoint with a unit test"
# spec → tasks → coder↔reviewer → PR → CI → merge to main
# Stalled tasks abandon locally with a full audit trail; the rest of the run
# proceeds. CI red leaves the PR open and exits non-zero.
```

Requires: `gh` CLI authenticated (`gh auth login`) and a configured git remote.
Stalled tasks land under `.aios/runs/<id>/abandoned/<task>/` for later review.

## What a run actually produces

```
.aios/runs/2026-04-24T10-12-03/
└── 001-add-health-endpoint/
    ├── round-1/
    │   ├── coder.prompt.txt        ← exact prompt sent to Claude
    │   ├── coder.response.raw      ← full raw stdout from `claude`
    │   ├── coder-text.txt          ← extracted assistant message
    │   ├── verify.json             ← test/lint/typecheck/build results
    │   ├── reviewer.prompt.txt     ← exact prompt sent to Codex
    │   ├── reviewer.response.raw   ← full raw stdout from `codex`
    │   ├── reviewer-response.json  ← parsed verdict
    │   └── mcp-calls.json          ← every MCP tool call
    ├── round-2/
    │   └── ...
    └── report.md                   ← human-readable task report
```

Nothing in that directory can be reconstructed from the code alone — and
nothing is missing. A future auditor (you, your teammate, your CI) sees the
same model inputs and outputs AIOS did.

## When things go wrong

Stuck tasks do not loop forever and do not silently abandon. Example output:

```
2 task(s) blocked:
  003-migrate-schema: [NEEDS HUMAN] stall_no_progress: 3 consecutive rounds
    raised identical review issues; 1 escalation(s) exhausted; unmet criteria:
    c2 (backwards-compat check fails), c3; blocking issues: schema.sql:
    missing default for added NOT NULL column | migration_test.go:
    rollback path untested
  007-refactor-auth: upstream_blocked (root cause: 003-migrate-schema)
```

The structured `BlockReason` codes (`stall_no_progress`, `max_rounds_exceeded`,
`rebase_conflict`, `rebase_review_rejected`, ...) are stable and scriptable.

## How AIOS compares

| Capability | **AIOS** | Aider | Sweep | Cline / Continue |
|---|:---:|:---:|:---:|:---:|
| Cross-model review enforced in code | **Yes** | No (single model) | No (single model) | No (single model) |
| Per-task git worktree isolation | **Yes** | No (in-place edits) | Branch-level | No (in-place) |
| Full prompt+response audit on disk | **Yes** | Partial (chat log) | Partial | No |
| Verify failures fed to reviewer | **Yes** | Manual | Manual | Manual |
| Structured, machine-readable block reasons | **Yes** | No | No | No |
| Stall detection + hard-constraint retry | **Yes** | No | No | No |
| Merge queue with rebase re-verify | **Yes** | No | No | No |
| Spec-first task DAG | **Yes** | No | Issue-driven | No |

The value of AIOS is not "another wrapper around an LLM CLI" — it's that every
single-model failure mode that makes autonomous coding unsafe has a named,
enforced countermeasure that shows up in both the config and the code.

## Commands

| Command | Purpose |
|---|---|
| `aios init` | Bootstrap the repo; autodetect verify commands. |
| `aios new "<idea>"` | Brainstorm → spec → task decomposition. |
| `aios run` | Iterate pending tasks; coder↔reviewer loop; auto-merge to `aios/staging`. |
| `aios status` | Print current task list with status. |
| `aios resume <id>` | Unblock a blocked task with a note. |

## Configuration highlights

The fields you will most often touch (full schema is the Go struct in
[`internal/config/config.go`](internal/config/config.go)):

```toml
schema_version = 1

[project]
base_branch    = "main"
staging_branch = "aios/staging"

[engines]
coder_default    = "claude"    # must differ from reviewer_default
reviewer_default = "codex"     # validated at load time

[budget]
max_rounds_per_task       = 5
max_tokens_per_task       = 200000
max_wall_minutes_per_task = 30
stall_threshold           = 3  # consecutive identical-issue rounds
max_escalations           = 1  # hard-constraint retries before blocking

[parallel]
max_parallel_tasks = 4
max_tokens_per_run = 1_000_000

[verify]
test_cmd      = "go test ./..."
lint_cmd      = "go vet ./..."
typecheck_cmd = ""
build_cmd     = "go build ./..."
```

## MCP servers

AIOS speaks MCP for external context (GitHub, docs, custom adapters). Configure
servers once in `.aios/config.toml`; tasks opt in with `mcp_allow:` in their
frontmatter. Default is deny-all — a task with no `mcp_allow` cannot reach any
MCP server.

```toml
[mcp.servers.github]
binary = "github-mcp-server"
args = ["stdio"]
env = { GITHUB_TOKEN = "${env:GITHUB_TOKEN}" }
allowed_tools = ["search_code", "get_pr"]
```

```yaml
---
id: 003-add-login
kind: feature
mcp_allow: [github]
acceptance:
  - works
---
```

Every MCP tool call is recorded in `.aios/runs/<run-id>/task-<id>/round-N/mcp-calls.json`.

## Project status

AIOS is pre-1.0. The closed-loop core (cross-model pairing, per-task worktrees,
verify↔review, escalation, full audit persistence) is implemented and covered
by unit and integration tests. Nightly end-to-end tests drive real Claude and
Codex through a small corpus of scenarios; see [`docs/e2e-setup.md`](docs/e2e-setup.md).

Known limitations in the current release:

- Auto-decompose for stuck tasks is shipping in v0.3.0; in autopilot mode (v0.2.0)
  stalled tasks are abandoned with a full audit trail rather than blocking the run.
- `--sandbox` (container isolation) remains stubbed; per-task `git worktree`
  isolation continues to be the v0.x story.
- MCP call failures are surfaced in audit logs; surfacing them inside the
  reviewer prompt is shipping in v0.3.1.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for dev setup, PR checklist, and
commit style. Issues are welcome; features should start with an issue so we
can align on scope before code lands.

## License

MIT. See [`LICENSE`](LICENSE).
