# AIOS

> **Two AIs build your code. A different one checks it. Every prompt, response, and failure is written to disk.**

AIOS drives **Claude CLI** and **Codex CLI** as a coder↔reviewer pair over a
spec-driven task queue. Each task runs in its own `git worktree` on a dedicated
branch. Approved work lands on `aios/staging`. You merge to `main` when you're
ready — the only human step in the loop.

---

## Three ways to drive AIOS

```
aios                       # interactive REPL — talk, refine, /ship when ready
aios "build X"             # one-shot spec → .aios/project.md, no execution
aios ship "build X"        # full pipeline: specgen → decompose → execute → PR → merge
```

All three run the same dual-AI specgen pipeline (Claude draft + Codex draft →
Codex merge → Claude polish → cross-model critique → optional refine). The
difference is what happens after the spec lands.

For scripts: `aios -p "build X"` writes the polished spec to stdout, no side
effects.

## Why AIOS exists

Two concrete goals:

1. **Better plans, specs, code, and workflow than Claude or Codex CLI alone.**
   Two models write the spec, a third pass merges them, a fourth polishes.
   The same cross-model discipline runs through execution: the engine that
   wrote the code is never the one reviewing it.
2. **Less human input than Claude or Codex CLI.** A single prompt with
   `aios ship` runs spec → tasks → coder↔reviewer → PR → merge end-to-end. The
   REPL collapses spec refinement into a chat instead of repeated re-prompts.

Single-model coding loops fail the same way every time: the model that wrote
the code is the one reviewing it, and so misses the exact class of errors it
just introduced. The only fix that holds up is structural:

- **The engine that writes is not the engine that reviews — ever.** Checked
  at config load *and* at runtime; an AIOS run refuses to start when
  `coder_default == reviewer_default`.
- **Cross-model critique on every spec.** After polish, the engine NOT used
  for polish scores the spec on a 0–12 rubric; below threshold triggers one
  refine cycle on the polish engine. Hallucinations and gaps that the same
  model can't see in its own output get caught by the other.
- **Every round's full prompt and raw response is persisted** before the next
  round begins. You can reconstruct exactly what each model saw and said,
  without re-running anything.
- **Each task is physically isolated** in its own `git worktree` on
  `aios/task/<id>`. Parallel tasks cannot contaminate each other, and your
  working checkout is never touched.
- **Verify failures feed the reviewer as blocking issues.** Approved-but-red
  code cannot merge. Stuck loops stop and tell you why — with the reviewer's
  top unresolved issues in the block reason.
- **Spec-level escalation when execution fails wholesale.** When multiple
  sibling tasks abandon with overlapping reviewer issues, AIOS regenerates
  the spec with the failure feedback folded in and retries — once per ship.

## Core advantages

| Advantage | How AIOS does it |
|---|---|
| **Cross-model review (mandatory)** | Config rejects `coder==reviewer`; runtime `engine.PickPair` rechecks. One engine's blind spots get caught by the other. |
| **Cross-model spec critique** | After polish, the engine NOT used for polish scores the spec on a 0–12 rubric. Score < threshold (default 9) → one refine cycle on the polish engine. Catches hallucinated APIs and vague requirements before any code is written. |
| **Full per-round audit trail** | `coder.prompt.txt`, `coder.response.raw`, `reviewer.prompt.txt`, `reviewer.response.raw`, `verify.json`, `reviewer-response.json` persisted per round. |
| **Per-task `git worktree` isolation** | Every task gets `aios/task/<id>` on its own checkout. Startup GC sweeps orphans from crashed prior runs; branches preserved for history. |
| **Verify↔review closed loop** | Red verify is folded into reviewer issues as synthetic blockers. Approval requires all criteria satisfied *and* all checks green. |
| **Three-tier stall recovery** | Stall → hard-constraint retry round → auto-decompose into sub-tasks (Claude+Codex propose, reviewer synthesises) → spec-level respec when sibling tasks abandon with overlapping issues → `[NEEDS HUMAN]` block with structured reasons. |
| **Engine-level retry** | Transient `claude` / `codex` failures retried up to 3× with exponential backoff. Failed attempts recorded in `coder.attempts.json` / `reviewer.attempts.json`. |
| **Round-history compression** | Once a chain exceeds 2 rounds, older context is compressed (algorithmic by default, optional LLM strategy) into `compressed-prior.txt`, keeping later rounds inside token budget without losing prior decisions. |
| **Deny-by-default MCP scoping** | Per-task `mcp_allow` intersected with run-wide config. Every MCP call logged to `round-N/mcp-calls.json`. |

## Pipeline

```
   your prompt
       │
       ▼
  specgen (draft → merge → polish → cross-model critique → optional refine)
       │
       ▼
  .aios/project.md ──► decompose ──► task DAG
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

For an internal tour, see [`docs/architecture.md`](docs/architecture.md).

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
aios doctor                        # one-shot preflight: engines, auth, repo, config
aios ship "Add a /health endpoint with a unit test"
# specgen → tasks → coder↔reviewer → PR → CI → merge to main, no further prompts.
```

If you want to inspect the spec before anything ships, drop `--ship`:

```bash
aios "Add a /health endpoint with a unit test"
# writes .aios/project.md and exits. Edit, then `aios ship` (no prompt) ships
# the existing spec, or run `aios` for an interactive refinement loop.
```

## Command index

| Command | What it does |
|---|---|
| `aios` | Interactive REPL. Each turn produces a unified spec via the dual-AI pipeline; `/ship` hands off to the autopilot. |
| `aios "<prompt>"` | One-shot specgen. Writes `.aios/project.md` and exits. |
| `aios ship "<prompt>"` | Full pipeline: specgen → decompose → execute → PR → merge. |
| `aios -p "<prompt>"` | Print polished spec to stdout. No project.md write, no side effects. |
| `aios --continue [<id>]` | Resume the latest REPL session, or a specific session id. |
| `aios init` | Bootstrap `.aios/config.toml` for the current repo. |
| `aios doctor` | One-shot preflight — engines, auth, git, config, smoke-test. |
| `aios run` | Iterate over pending tasks; coder↔reviewer per task. |
| `aios duel <task>` | Race Claude and Codex on the same task; reviewer picks the winner. |
| `aios review <pr>` | Cross-model PR review; optional comment-back via `gh pr comment`. |
| `aios serve` | Issue-bot daemon — watches `aios:do`-labeled GitHub issues. |
| `aios cost [run-id]` | USD estimate per run from the on-disk audit trail. |
| `aios lessons` | Mine `.aios/runs/` for recurring reviewer-issue patterns. |
| `aios mcp scaffold <preset>` | Append a ready MCP server block (github / fs-readonly / playwright). |
| `aios unblock`, `aios status` | Standard run-management helpers. |

## Interactive mode

Run `aios` with no subcommand for an interactive session. Same shape as `claude`
or `codex`.

```
$ aios
aios — type a requirement, blank line to submit. /help for commands.
> build a CLI for syncing notebooks across machines
>
  · draft-claude …
  · draft-codex …
  · merge …
  · polish …
Spec updated (84 lines). /show to view, /ship to implement, or refine with another message.
> tighten the section on conflict resolution
>
  · ...
> /ship
shipping spec to autopilot…
```

Each turn runs a multi-stage dual-AI pipeline:

1. Claude drafts spec A.
2. Codex drafts spec B (in parallel with stage 1).
3. Codex merges A and B into one spec, with initial polish.
4. Claude does a secondary refinement on the merged spec.
5. **Cross-model critique** — Codex (the engine NOT used for polish) scores
   the result on a 0–12 rubric covering coverage, specificity, and
   feasibility.
6. **Optional refine** — if score is below `[specgen] critique_threshold`
   (default 9), Claude runs one refine cycle using the critique as
   guidance. Otherwise the polished spec ships as-is.

The final spec is written to `.aios/project.md`. Every intermediate stage —
`1-draft-claude.md`, `2-draft-codex.md`, `3-merge.md`, `4-polish.md`,
`5-critique.md`, `5-score.json`, and (when triggered) `6-refine.md` — lands
under `.aios/runs/<run-id>/specgen/` so you can see what each stage
contributed.

Slash commands: `/show`, `/clear`, `/help`, `/ship`, `/exit`.

Resume: `aios --continue` picks up the latest session, `aios --continue <session-id>`
picks a specific one. Sessions persist to `.aios/sessions/<id>/session.json`
after every turn. (Distinct from `aios unblock <task-id>`, which unblocks a stuck task.)

Failure handling: if one drafter dies, the surviving engine produces the spec
alone and a warning is printed. If the merge step fails, the longer of the two
drafts becomes the merged version. If polish fails, the merged version is the
final. With either Claude or Codex missing from PATH the REPL refuses to launch
— run `aios doctor` to diagnose.

## Ship mode (one prompt to merged PR)

```bash
aios ship "Add a /health endpoint with a unit test"
```

Runs specgen, writes `.aios/project.md`, decomposes into task files, runs the
coder↔reviewer loop, opens a PR, polls CI, and merges on green. Same plumbing
as the REPL's `/ship` command and as `aios serve` — they all call the same
`ShipPrompt` entry point.

Requires: `gh` CLI authenticated (`gh auth login`) and a configured git remote.
Stalled tasks land under `.aios/runs/<id>/abandoned/<task>/` for later review.

### Auto-decompose for stalled tasks

When a task stalls — repeated rounds raise the same unresolved reviewer issues
even after escalation — ship mode tries to split it before giving up:

1. Claude and Codex each independently propose a 2–4 sub-task split.
2. Whichever engine reviewed the stuck task synthesises the two proposals
   into a single unified split.
3. Sub-tasks land in `.aios/tasks/<parent>.<n>.md`, the parent's frontmatter
   is marked `status: decomposed`, and the run continues with the children.

Recursion is bounded by `[budget] max_decompose_depth` (default 2, hard cap 3).
A child that re-stalls at the depth cap abandons rather than recursively splits.
If both engines error, or the synthesizer emits fewer than 2 sub-tasks, the
parent abandons via the audit-trail path described above.

### Spec-level respec on overlapping abandons

When auto-decompose has already failed and **two or more sibling tasks abandon
with reviewer-issue fingerprints that overlap** (pairwise Jaccard ≥
`[budget] respec_min_overlap_score`, default 0.5), the failure is no longer
local — the spec itself is suspect. AIOS regenerates the spec with the failure
feedback folded in and retries the run once.

- Triggered at most once per ship (`respec_on_abandon`, default `true`).
- Audit artifacts written to `.aios/runs/<run-id>/respec/`:
  `feedback.md` (issues fed into the new spec), `new-project.md` (regenerated
  spec), `old-tasks/` (the abandoned siblings preserved for inspection).
- If the second pass also abandons, the run reports `[NEEDS HUMAN]` —
  re-running won't help.

## Duel mode (race Claude and Codex on one task)

Want to know which engine is stronger on a specific kind of change — a
security fix, a data migration, a perf-critical hot path? Run a duel:

```bash
aios duel "Add a rate limiter to the /login endpoint with 10 req/min per IP"
```

Both engines run as coders in parallel, each in its own ephemeral
worktree. The reviewer-default engine then reads both diffs and picks a
winner on three axes: correctness, minimality, clarity. No commits are
made; both worktrees are torn down on exit. Pass `--apply` to copy the
winning diff onto your working tree as uncommitted changes.

This is something neither Claude CLI nor Codex CLI can do alone.

## PR review mode (cross-model review of any GitHub PR)

```bash
aios review 42                      # number — resolves against current repo
aios review https://github.com/owner/repo/pull/42
aios review 42 --post               # also publish via `gh pr comment`
```

Both engines review the diff in parallel; the reviewer-default engine
synthesises one consolidated comment. The merged verdict is the more
conservative of the two (request-changes wins over comment-only wins
over approve), and disagreements between the two reviewers are surfaced
rather than hidden.

## Cost telemetry

Every run lands its raw token usage in `.aios/runs/<id>/`. To turn that
into a dollar estimate at any time:

```bash
aios cost              # cost the most recent run
aios cost <run-id>     # cost a specific run
aios cost --all        # per-run table plus grand total
```

Pricing is a hardcoded table (`internal/cost/pricing.go`) — treat the
output as an estimate, not an invoice. The numbers stay correct on
year-old run directories because every input is on disk.

## Lessons learned

After a few runs, AIOS has a sample of what the reviewer keeps catching:

```bash
aios lessons
```

Aggregates every reviewer issue across every run into a 30-second report:
top issue categories, top recurring note shapes, hot-spot files, noisiest
runs. Use it to decide where editing `coder.tmpl`, the spec, or the
codebase itself will pay back the most review-loop time.

## MCP scaffold

```bash
aios mcp list                       # show all preset bodies
aios mcp scaffold github            # append [mcp.servers.github] to config
aios mcp scaffold fs-readonly       # local filesystem, read-only
aios mcp scaffold playwright        # headless browser
```

Idempotent. Re-running with the same preset is a no-op.

## Serve mode (issue bot)

`aios serve` watches a GitHub repo for issues labeled `aios:do` and ships each
one. The bot opens the PR, comments back on the issue with the PR link, closes
the issue on merge, and files an `aios:stuck` issue with the audit trail when
the run abandons.

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
max_concurrent_issues = 1   # clamped to 1 in current release
```

State persists at `.aios/serve/state.json`. A killed daemon reconciles on
restart: `aios:in-progress` issues with no local state are released back to
`aios:do` for retry.

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

Task MCP is attached to the execution coder only. AIOS writes an engine-scoped
MCP config for that coder, records every MCP call, and passes MCP failures into
the reviewer prompt. Reviewers, spec drafters, PR reviewers, and other
auxiliary parallel stages do not receive `--mcp-config`; this prevents two
CLIs from starting the same socket-binding MCP server for one task.

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

What v0.2.0 added on top of v0.1:

- **Cross-model spec critique** with score-gated refine cycle (`[specgen]
  critique_enabled`, `critique_threshold`).
- **Round-history compression** keyed off chain length (`[budget]
  compress_history`, `compress_after_rounds`, `compress_target_tokens`);
  default flipped to `true`.
- **Engine retry** layer for transient `claude` / `codex` failures
  (`[engines.*] retry_max_attempts`, `retry_base_ms`, `retry_enabled`).
- **Spec-level respec** on overlapping sibling abandons (`[budget]
  respec_on_abandon`, `respec_min_overlap_score`).

Known limitations in the current release:

- `--sandbox` (container isolation) remains stubbed; per-task `git worktree`
  isolation continues to be the v0.x story.
- MCP call failures are surfaced both in the per-round audit
  (`mcp-calls.json`) and inline in the reviewer prompt — the reviewer can
  distinguish "coder ignored a constraint" from "coder couldn't reach
  external context" without spawning its own MCP server copy.
- `aios serve` ships sequential-only. The `[concurrency]
  max_concurrent_issues` config knob exists but is clamped to 1 internally
  pending per-issue `.aios/` workspace isolation.
- No empirical eval harness yet — quality claims rest on the structural
  guarantees above (cross-model review, critique, respec, audit trail), not
  on a benchmark.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for dev setup, PR checklist, and
commit style. Issues are welcome; features should start with an issue so we
can align on scope before code lands.

## License

MIT. See [`LICENSE`](LICENSE).
