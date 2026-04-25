# AIOS Autopilot Roadmap — Design Spec

**Date:** 2026-04-25
**Status:** approved (awaiting spec-review)
**Owner:** MoonCodeMaster
**Repo state at authoring:** `main` @ `77ab919`, npm `@mooncodemaster/aios@0.1.1` (first usable release)

---

## Goal

Two parallel, load-bearing outcomes:

1. **Zero manual intervention** across the full lifecycle: idea → spec → tasks → coder↔reviewer → verify → PR → CI → merge to `main`.
2. **GitHub visibility / popularity** for the maintainer, achieved as a by-product of (1) — every autopilot run is a public PR; AIOS-on-AIOS dogfooding turns the repo's own commit history into the demo.

**Done when:** `aios autopilot "<idea>"` runs end-to-end and exits 0 after a clean merge on `main`; AND `aios serve` performs the same loop driven by GitHub-issue labels with AIOS itself as the day-1 user.

---

## State assessment (2026-04-25)

AIOS is **usable today** via `npm install -g @mooncodemaster/aios@0.1.1`. Build and `go vet` are clean on `main`. The prior 5-pain-point analysis (memory: `project_aios_painpoints_2026-04-24.md`) is now SOLVED or PARTIAL except:

- P0-2 tier 2 (auto-decompose) — deferred. **Closed by M2 of this roadmap.**
- P1-4 (MCP-failure → reviewer) — open. **Closed by M3.**
- P1-6 (`--sandbox` Docker isolation) — stubbed. **Explicitly deferred to post-1.0.**
- P2-7 / P2-8 (demo, comparison content, GH templates) — open. **Closed by M3 + M5.**

The new gap this roadmap exposes that the prior analysis did not list: AIOS today has **two by-design human gates** — the `aios new` confirmation prompt (`internal/cli/new.go:82`) and the manual `git merge aios/staging`. Both are incompatible with the zero-intervention goal and are removed in M1.

---

## Approach

**Linear ladder with dogfood detour.** Sequential milestones; each is independently shippable. Dogfooding AIOS on the AIOS repo starts the moment M1 lands, so visibility content (PRs trailered with `Co-authored-by: aios`) accumulates along the way rather than as a separate phase.

Approach selected over: "demo-first" (too fragile to ship under load) and "two-track parallel" (too much context-switching for a sole maintainer).

---

## Milestone roadmap

| # | Release | Theme | Headline capability | Visibility byproduct |
|---|---|---|---|---|
| **M1** | v0.2.0 | One-shot autopilot | `aios autopilot "<idea>"` end-to-end: spec → tasks → coder↔reviewer → PR → CI → merge → exit. Removes both human gates. Abandoned tasks drop locally with full audit trail. | First AIOS-on-AIOS PR (M2 sub-task #1). `Co-authored-by: aios` trailer established. |
| **M2** | v0.3.0 | Auto-decompose (dual-engine) | Stuck tasks trigger parallel Claude+Codex decompose calls; a synthesizer (the reviewer of the stuck task) merges both proposals into a unified sub-task list. Closes the only remaining P0 item. | Asciicast: deliberately-too-large issue auto-splits and converges. |
| **M3** | v0.3.1 | Hardening polish | MCP-failure → reviewer prompt; `.github/` issue+PR templates + Discussions; `docs/architecture.md`; 60–90s autopilot asciicast in README. `--sandbox` explicitly deferred. | "Why AIOS" landing section; comparison blog post draft. |
| **M4** | v0.5.0 | `aios serve` issue-bot | Long-running (or `--once` cron) daemon. Watches GitHub issues with label `aios:do`, runs autopilot per issue, opens PRs, comments back, files `aios:stuck` audit-trail issues on abandon. Crash-safe via `.aios/serve/state.json`. AIOS-on-AIOS from day 1. | Show HN: "AIOS develops AIOS in public — every recent PR was opened by the bot." |
| **M5** | — | Public launch | Comparison blog, Show HN, r/golang + r/ChatGPTCoding. AIOS commit history is the demo. | The compounding step. |

### Out of scope (explicit)

- IDE plugins (VS Code, JetBrains).
- Self-hosted "AIOS Cloud" / multi-tenant infra.
- Web UI / dashboard.
- Auto-generation of *ideas* (a queue replenisher, autonomous goal synthesis).
- Multi-repo `aios serve` (single-repo only in M4).
- `--sandbox` Docker isolation (deferred to post-1.0; per-task `git worktree` is the v0.x isolation story).

---

## M1 — One-shot autopilot

### Components

| New / changed | Path | Responsibility |
|---|---|---|
| **NEW** `aios autopilot` command | `internal/cli/autopilot.go` | Top-level shorthand: runs `new --auto` then `run --autopilot --merge`. |
| `aios new --auto` flag | `internal/cli/new.go` (modify) | Skip the `Confirm and commit?` gate at `internal/cli/new.go:82`; commit spec+tasks unconditionally. Default off; legacy interactive `aios new` unchanged. |
| `aios run --autopilot --merge` flags | `internal/cli/run.go` (modify) | `--autopilot`: abandon-on-stall instead of `[NEEDS HUMAN]`-stop. `--merge`: triggers the finalizer. Two flags so users can audit autopilot runs without auto-merging. |
| **NEW** GitHub host adapter | `internal/githost/githost.go` | `gh` CLI wrapper: `OpenPR(base, head, title, body)`, `WaitForChecks(pr, timeout)`, `MergePR(pr, mode)`. One real impl, one fake for tests. Mirrors `engine.Engine` shape. |
| **NEW** autopilot finalizer | `internal/cli/run.go` (or `autopilot.go`) | After `RunAll`: open PR `aios/staging → main`, poll checks, merge on green, write `autopilot-summary.md`. |
| **NEW** abandoned-task handler | `internal/run/abandoned.go` | Write `.aios/runs/<run>/abandoned/<task>/{report.md,full-trail.json}`; flip task frontmatter to `status: abandoned`; continue the run. |
| `stallThreshold` config wiring | verify config flows through `dep.StallThreshold` (`run.go:317`); fix any orchestrator default that shadows config. |

### Data flow

```
aios autopilot "<idea>"
  │
  ├─► aios new --auto             ← no confirm gate
  │     claude.brainstorm  → .aios/runs/<id>/brainstorm.md
  │     claude.spec-synth  → .aios/project.md
  │     codex.decompose    → .aios/tasks/NNN-*.md
  │     git commit on aios/staging
  │
  ├─► aios run --autopilot --merge
  │     [unchanged] worktree-isolated coder↔reviewer per task
  │     [unchanged] verify↔review closed loop, escalation tier 1+3
  │     [NEW] on abandon: write .aios/runs/<id>/abandoned/<task>/
  │            mark task abandoned; continue rest of run
  │     [unchanged] MergeQueue rebases converged tasks onto aios/staging
  │
  └─► autopilot finalizer  (only with --merge)
        if no tasks converged                    → exit 2, no PR
        else:
          githost.OpenPR(staging→main)           → PR URL
          githost.WaitForChecks(PR)              → green | red | timeout
          green   → githost.MergePR(--squash --delete-branch); exit 0
          red     → leave PR open; exit 2 with PR URL
          timeout → leave PR open; exit 2 with PR URL
        write .aios/runs/<id>/autopilot-summary.md
```

### Decisions

- **PR merge mode:** `--squash --delete-branch` — one tidy commit per autopilot run on `main`.
- **`gh` CLI dependency:** required as a preflight for autopilot mode (`gh` on PATH, `gh auth status` clean, remote exists, `aios/staging` ancestor of `main`).
- **No new auth code:** all GitHub auth flows through the user's existing `gh` session.

### Error handling (zero-intervention invariant: never block on stdin)

| Failure | Behavior |
|---|---|
| Preflight fails (`gh` missing, not authed, no remote) | Exit 2 with explicit message. No model invocation. |
| `gh pr create` fails | Converged work stays on `aios/staging`; user can manually `gh pr create` later. Summary file logs the failure. |
| CI red | PR stays open; AIOS exits 2; summary cites PR URL. **Never auto-merge red.** |
| CI timeout (configurable, default 30 min) | Same as CI red; distinct exit message. |
| All tasks abandoned | No PR opened; audit trail under `.aios/runs/<id>/abandoned/`. Exit 2. |
| Partial run interrupted | `aios/staging` and abandoned dir intact; re-running with same spec is safe (idempotent). |

### Testing strategy

| Layer | Test | Coverage |
|---|---|---|
| Unit | `internal/githost/githost_test.go` | `gh` invocation strings; PR-state parsing; check-status decoding. Fake exec. |
| Unit | `internal/run/abandoned_test.go` | Abandoned artifact layout; frontmatter status flip is idempotent. |
| Integration | `test/integration/autopilot_oneshot_test.go` | Full flow with fake engines + fake githost. PR opened, checks polled, merge invoked on green, no merge on red, abandon path writes artifacts and continues. |
| E2E | `test/e2e/autopilot_e2e_test.go` (gated by `AIOS_E2E=1`, nightly) | Real `gh` against a throwaway repo. Smoke only. |
| Manual | Dogfood on AIOS itself | M2's first sub-task is the first AIOS-on-AIOS autopilot PR. |

---

## M2 — Auto-decompose (dual-engine + synthesizer)

### The shape

When a task has exhausted escalations and depth budget allows, the orchestrator returns `StateDecomposeRequested`. The CLI handler issues **two parallel decompose calls** (Claude + Codex), then a **third synthesis call** that merges both proposals. The merged sub-task list goes into the live scheduler. Cross-model thesis preserved at the planning step too.

```
StateDecomposeRequested
  │
  ├── parallel ─┬── claude.Invoke(decompose-stuck.tmpl)  → proposalA
  │            └── codex.Invoke(decompose-stuck.tmpl)    → proposalB
  │
  ├── synthesizer.Invoke(decompose-merge.tmpl, {parent, proposalA, proposalB})
  │     synthesizer = reviewer of the stuck task (cross-model)
  │     → unified sub-task blocks (===TASK=== format, same as aios new)
  │
  └── parseTaskBlocks → write .aios/tasks/<parent>.<n>.md → pool.InsertTasks(...)
```

### Components

| Component | Path | Responsibility |
|---|---|---|
| **NEW** decompose state + context | `internal/orchestrator/decompose.go` | `StateDecomposeRequested`; `DecomposeContext{ Task, IssuesByRound, LastDiff, ReviewHistory }`. Returned instead of blocking when `task.Depth < cfg.Budget.MaxDecomposeDepth`. |
| **NEW** decompose handler | `internal/cli/run.go` (extension) | Parallel goroutines for both `engine.Invoke`; collect both; pick synthesizer = reviewer; render `decompose-merge.tmpl`; parse sub-task blocks; write task files; `pool.InsertTasks(...)`. |
| **NEW** dynamic scheduler insertion | `internal/orchestrator/pool.go` (modify) | `Pool.InsertTasks(parentID, children)` — atomic: appends pending; rewires dep map (parent→blocked-by-children, downstream-of-parent→blocked-by-all-children). |
| **NEW** prompt template | `internal/engine/prompts/decompose-stuck.tmpl` | Inputs: parent body, deduped reviewer issues, last diff, "why stuck" summary. Output: `===TASK===` blocks (same contract as `aios new` decompose). |
| **NEW** prompt template | `internal/engine/prompts/decompose-merge.tmpl` | Inputs: parent + ProposalA + ProposalB. Output: same `===TASK===` blocks; instructions: "merge overlaps, preserve unique cuts, reject contradictions, smallest viable set." |
| Spec model fields | `internal/spec/task.go` (modify) | Add `Depth int`, `ParentID string`, `DecomposedInto []string`; allow `Status: "decomposed"`. |
| Config field | `internal/config/config.go` (modify) | `[budget] max_decompose_depth = 2` (default 2; hard cap 3 in code). |

### Decisions

| Decision | Choice | Why |
|---|---|---|
| Decompose engines | **Both** in parallel | User's design call: avoid single-model bias at the planning step. |
| Synthesizer | Reviewer of the stuck task | Preserves cross-model rotation; no new config knob. Mild self-bias risk accepted (synthesizer is also one of the two proposers; sees the *other* model's proposal alongside its own). |
| Recursion depth | Default 2; hard cap 3 | A sub-task that re-stalls and re-decomposes is almost certainly a spec problem the model can't solve — fail loudly instead of fanning out. |
| Parent worktree on decompose | Throw away; sub-tasks branch from `aios/staging` | Reusing the parent branch would feed broken code into sub-tasks. Branch preserved (no `git branch -D`) for the audit trail. |
| Sub-task IDs | `<parent-id>.<n>` (e.g. `004.1`, `004.2`) | Sortable; lineage-obvious; doesn't collide with existing `001`/`002` numbering. |
| Sub-task `depends_on` | Inherits parent's `depends_on` | Sub-tasks must respect what the parent was waiting on. |
| Downstream tasks of decomposed parent | Implicit blocked-by-all-children, computed in scheduler | User doesn't have to rewrite the task graph after a split. |
| ≤1 sub-task returned | Treated as abandon | A 1-task split is the model giving up dressed as productivity. |

### Error handling

| Failure | Behavior |
|---|---|
| Proposal A errors, B succeeds | Skip synthesis; use B directly. Log "fell back to single-model decompose: <engine>" in summary. |
| Both proposals error | Abandon parent. Don't synthesize from nothing. |
| Both succeed, synthesizer errors | Deterministic fallback merge: union; dedupe by `id:`; on collision prefer the proposal whose author was the reviewer. Logged as "synthesis fallback". |
| Synthesizer returns ≤1 task | Treat as abandon. |
| Malformed output (no `===TASK===`, no `id:`) | Abandon parent. **Do not retry decompose** — that's how runaway costs happen. |
| Decompose model error (timeout, transport) | One retry, then abandon parent. |
| Sub-task ID collision | Write to `<parent>.<n>.<random>`; log warning; don't fail the run. |
| All sub-tasks abandon | Parent's `decomposed_into` retained for audit; parent surfaces as `decomposed-but-empty` in `autopilot-summary.md`. PR still opens for whatever else converged. |

### Cost framing (honest)

Dual-decompose triples LLM calls **for the decompose step only**, which fires only when a task has already exhausted retries and escalations. Clean run, no stuck tasks: zero extra cost. Run with one stuck task: 2 extra invokes. ~2–5% per stuck task; negligible per run. Trade is worth it for cross-model integrity.

### Testing strategy

| Layer | Test | What it proves |
|---|---|---|
| Unit | `decompose_test.go` | Context construction; sub-task parsing; depth-cap arithmetic; scheduler insertion atomicity. |
| Unit | `pool_test.go` (extend) | `InsertTasks` rewires dependencies correctly; downstream tasks of decomposed parent wait for all children. |
| Integration | `autopilot_decompose_test.go` | Fake engines: parent fails 3× → 2-task decompose → both children converge. Assert: parent `decomposed`; children `converged`; PR contains both diffs; `autopilot-summary.md` lists the decomposition. |
| Integration | `autopilot_decompose_depth_cap_test.go` | Stuck at depth 0 *and* depth 1; assert depth-1 child abandons (no further decompose); parent ends `decomposed-but-empty`. |
| Integration | `autopilot_decompose_partial_failure_test.go` | Proposal A errors → synthesis skipped → B used directly. Both error → parent abandoned. Synthesizer errors → deterministic fallback merge. |
| E2E | Deferred to M3 | Real-LLM decompose is expensive; ship via M2 dogfooding instead. |

---

## M3 — Hardening polish

No architecture changes. Bundles lingering P1/P2 items.

| Item | Scope |
|---|---|
| **MCP-failure → reviewer prompt (P1-4)** | Capture failed MCP tool calls per round (already in `mcp-calls.json`); append `## MCP failures this round` to `reviewer.tmpl`. Reviewer can distinguish "coder ignored constraint" from "coder couldn't reach docs server." |
| **Issue + PR templates (P2-8)** | `.github/ISSUE_TEMPLATE/{bug.yml,feature.yml,question.yml}`; `.github/PULL_REQUEST_TEMPLATE.md`; enable Discussions tab. README "Why AIOS" section pulled to top. |
| **`docs/architecture.md` (P2-7)** | One page: orchestrator state machine; merge queue; autopilot finalizer; decompose pipeline. Diagram inline. |
| **Asciicast** | One 60–90s `aios autopilot "<idea>"` recording against a toy repo; embedded in README. |
| **`--sandbox` Docker isolation (P1-6)** | **Explicitly deferred to post-1.0.** Per-task `git worktree` is the v0.x isolation story. Closed in release notes. |

Tests: small unit test for MCP-failure section rendering with/without failures. Everything else is docs + GitHub config.

---

## M4 — `aios serve` issue-bot

### The shape

Long-running (or `--once` cron-driven) process that watches GitHub issues labeled `aios:do`, picks them up, runs the M1+M2 autopilot pipeline per issue, and reflects state back through labels and comments. Each issue → potentially one PR → potentially one merge. Crash-safe via persistent state file. AIOS itself is the day-1 deployment.

### Components

| Component | Path | Responsibility |
|---|---|---|
| **NEW** serve command | `internal/cli/serve.go` | `aios serve [--once] [--repo OWNER/NAME]`. Daemon mode loops; `--once` does one poll cycle and exits (cron-friendly). |
| **NEW** GitHub issue adapter | `internal/githost/issues.go` (extends M1 githost) | `ListLabeled(label) → []Issue`; `AddLabel`, `RemoveLabel`, `AddComment`, `OpenIssue`, `LinkPRToIssue`. Via `gh` CLI. |
| **NEW** per-issue runner | `internal/cli/serve_runner.go` | Issue → idea string → autopilot pipeline → label/comment reflection. |
| **NEW** persistent state | `.aios/serve/state.json` | `{ issueID: {runID, claimedAt, status} }`. Reconciled with GitHub labels at startup. |
| **NEW** serve config | `.aios/serve.toml` | `[repo] owner/name`; `[labels] do/in_progress/pr_open/stuck/done`; `[poll] interval_sec`; `[concurrency] max_concurrent_issues`. |
| Autopilot pipeline | unchanged | `aios serve` is a *driver* over the same M1 pipeline. |

### Data flow

```
aios serve --repo MoonCodeMaster/AIOS
  │
  ├── startup reconcile:
  │     in-progress on GitHub  ∩  .aios/serve/state.json
  │       both → resume run record (M1 crash-safe)
  │       only GitHub → orphan; release label
  │       only state.json → orphan; remove from state
  │
  └── poll loop (every cfg.poll.interval_sec, default 60s):
        candidates = githost.ListLabeled(cfg.labels.do)
                     − issues in state.json
        spawn up to (max_concurrent − inflight) workers:

           runIssue(issue):
             githost.RemoveLabel(issue, "aios:do")
             githost.AddLabel(issue,    "aios:in-progress")
             state.Add(issue.id, runID)
             idea = renderIdea(issue.title, issue.body)
             outcome = autopilot.Run(idea)
             match outcome:
               MERGED       → comment "Merged in #<PR>"; label "aios:done"
               PR_OPEN_RED  → comment "PR #<n> open; CI failing"; label "aios:pr-open"
               PR_TIMEOUT   → comment "PR #<n> open; CI still running"; label "aios:pr-open"
               ABANDONED    → openIssue("[aios:stuck] " + issue.title, auditTrail)
                              comment "Couldn't converge; trail in #<new>"
                              label "aios:stuck"
             state.Remove(issue.id)
```

### Decisions

| Decision | Choice | Why |
|---|---|---|
| Auth | `gh` CLI, already authed | M1 already requires it; no new auth code, no new failure mode. |
| State location | `.aios/serve/state.json` | Survives restarts. Single source of truth alongside (eventually consistent) GitHub label state. |
| Reconciliation | Labels authoritative for "GitHub view"; state.json authoritative for "AIOS view". Symmetric difference resolved at startup. | Either side can drift (process killed, label hand-edited). Both must converge. |
| Concurrency default | `max_concurrent_issues = 2` | Two issues at once is enough to exercise merge-queue rebase; more rarely useful for solo repos. |
| Issue → idea rendering | `title + "\n\n" + body` verbatim | Spec-synth call already does heavy lifting; pre-processing would mask context (code snippets, error logs). |
| `--once` mode | Ships in M4 | Cron + GitHub Actions users; same code path, inner loop runs once. |
| Multi-repo | Deferred | Premature for v0.5. Single-repo covers dogfood and 95% of solo users. |

### Error handling

| Failure | Behavior |
|---|---|
| GitHub rate limit (403/429) | Exponential backoff; never exit the daemon. |
| Process killed mid-run | On next start, reconcile catches orphan label, releases to `aios:do`; run record on disk shows `interrupted`. |
| Issue body unparseable as a coding task | Autopilot abandons cleanly; bot files `aios:stuck`. Healthy failure. |
| Two issues touching same files | Existing `MergeQueue`: rebase + re-verify + cross-model re-review. Already solved upstream — no new code in M4. |
| `gh` token expires | "Auth-broken" state; log loudly; refuse new claims; let in-flight finish. Never silently swallow. |

### Testing strategy

| Layer | Test | What it proves |
|---|---|---|
| Unit | `serve_runner_test.go` | Idea-rendering format; label-state-machine transitions; comment-body templates. |
| Unit | `state_test.go` | Reconcile logic on every drift combination. |
| Integration | `serve_oneshot_test.go` | Fake `gh` + fake autopilot. MERGED / ABANDONED / PR_OPEN_RED paths each tested. |
| Integration | `serve_concurrency_test.go` | Two issues claimed in parallel; merge-queue rebase exercised; both finish cleanly. |
| Integration | `serve_recovery_test.go` | Kill mid-flight; restart; reconcile releases orphan correctly. |
| Manual | Dogfood on AIOS itself | Day-1 user; every PR from M4 onward bot-authored. |

---

## M5 — Public launch

No engineering. Comparison blog post (AIOS vs. Aider/Sweep/Cline using the existing README table as the spine). Show HN with the autopilot asciicast as the headline. Cross-posts to r/golang and r/ChatGPTCoding. The compounding artifact is AIOS-on-AIOS commit history — strangers can scroll `git log` and see the bot's name.

---

## Risk register

| Risk | Severity | Mitigation |
|---|---|---|
| Auto-merge to `main` lands a regression that CI didn't catch | High | M1 mandates PR + CI green before merge. Branch protections inherit. CI red → never merge. The `--squash --delete-branch` PR is reverable in one click. |
| Dual-decompose token cost balloons on a pathological run | Medium | Decompose only fires on stuck tasks; recursion hard-capped at depth 3; per-task and run-level token budgets unchanged. |
| `gh` CLI dependency is a friction in CI environments | Low | Fallback path is documented (manual `git merge` with `--autopilot` minus `--merge`). M4 cron use case is exactly the GH Actions runner where `gh` is preinstalled. |
| Synthesizer self-bias toward its own proposal | Low | Synthesizer sees the *other* model's proposal alongside its own; instruction explicitly says "preserve unique cuts." Worst case: synthesis-fallback merge is deterministic. |
| `aios serve` floods the repo with `aios:stuck` issues during a bad run | Medium | M4 ships with `max_concurrent_issues = 2` default; rate-limited polling; `aios:stuck` issues are themselves first-class artifacts (this is the feature, not a bug). |
| Dogfooding on AIOS itself bricks the AIOS repo | High | M1's PR + CI gate catches regressions before merge; `aios/staging` is always rebased onto `main`; CI runs `go test ./...` which is the same suite users run. If catastrophic: `git revert` the squash commit. |

---

## Done criteria

- M1 done when: `aios autopilot "test feature"` on a clean repo opens a PR, waits for CI, fast-forwards `main`, exits 0; integration test green.
- M2 done when: a deliberately-stuck integration scenario triggers parallel decompose + synthesis, sub-tasks land, parent marked `decomposed`; both partial-failure paths covered by tests.
- M3 done when: MCP-failure surface lands in reviewer prompt with a unit test; `.github/` templates merged; `docs/architecture.md` published; asciicast embedded in README.
- M4 done when: `aios serve` runs against AIOS for 7 consecutive days without manual intervention; ≥1 issue → merged-PR cycle observed; recovery from kill -9 verified.
- M5 done when: launch posts published, AIOS commit history shows ≥10 bot-authored PRs.

---

## Appendix: decisions deferred for later spec rounds

- Sub-task `depends_on` semantics for the *decomposed* parent's downstream graph — implementation will document the precise semantics in the M2 plan.
- `aios serve` resume-on-restart for in-flight runs that were mid-coder-round when killed (M1 crash-safety covers state files; M4 plan will spell out reattachment).
- Whether `aios:done` should auto-archive the issue or leave it as a closed-with-label artifact (M4 decision).
