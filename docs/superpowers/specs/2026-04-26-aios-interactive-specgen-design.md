# AIOS Interactive Spec Generation — Design

**Date:** 2026-04-26
**Status:** Design approved; awaiting user review of this document before implementation plan.

## Goal

Make `aios` itself the user's interactive entry point — same usage shape as `claude` or `codex` CLIs — and have it produce a single unified spec/plan document that is meaningfully better than what either Claude CLI or Codex CLI produces alone. The "better" comes from a deterministic 4-stage dual-AI pipeline, not from a user-facing mode toggle.

## Non-goals

- No mode flags. There is no "short-termism" / "long-termism" / fast / cheap variant. One pipeline, always.
- No "pick one of three blueprints" UX. One document out, every time.
- No replacement of `aios new` / `aios architect` in this increment. The new REPL is purely additive; deprecation is a follow-up once the new flow proves out.
- No automatic retries on model-call failure.
- No real-CLI integration tests in CI (covered manually via existing `test/e2e/`).

## Product surface

Running `aios` with no subcommand and no positional args launches an interactive REPL. Inside, the user types natural-language requirements. Each turn runs the 4-stage pipeline and writes one unified spec/plan to `.aios/project.md`. Subsequent turns refine that spec (the prior spec + the full conversation history are fed back into the next pipeline run).

Slash commands inside the REPL:

- `/ship` — hand off the current `.aios/project.md` to the existing autopilot (decompose → run tasks → PR). Explicit beats natural-language detection because misfiring on this command is expensive.
- `/show` — print the current spec.
- `/clear` — discard session, start fresh.
- `/help` — list commands.
- `/exit` — leave the REPL.

Existing subcommands (`aios new`, `aios architect`, `aios run`, `aios autopilot`, etc.) continue to work unchanged. Only the bare `aios` invocation gets the new behavior.

**Pipeline output during a turn.** Quiet — a single spinner with stage labels (`drafting · Claude … drafting · Codex … merging · Codex … polishing · Claude`). The four intermediate drafts are written to disk for inspection but not streamed to the terminal. The final spec is summarized in one or two lines after the pipeline completes (e.g., "Spec updated (847 lines, 4 sections changed). Type more to refine, `/show` to view, `/ship` to implement.").

## The 4-stage pipeline

A new package `internal/specgen/` owns the chain. One exported function:

```go
func Generate(ctx context.Context, in Input) (Output, error)
```

`Input` carries:

- The user's accumulated request — current turn plus prior turns from the same REPL session.
- Project context — repo summary and the existing `.aios/project.md` if present.
- Engine adapters (`claude`, `codex`).

`Output` carries:

- `Final string` — the spec text after stage 4.
- `DraftClaude string`, `DraftCodex string`, `Merged string` — intermediate stages, for the run-dir audit trail.
- `Stages []StageMetric` — per-stage timing, token counts, and any partial-failure markers.

### The four stages

1. **Draft A — Claude.** Prompt: "Generate a complete spec/plan for the following requirement. Be opinionated, structured, and concrete." Independent — does not see Codex's draft.
2. **Draft B — Codex.** Identical prompt, run in parallel with stage 1 (single goroutine wait barrier between {1,2} and 3). Independent — does not see Claude's draft.
3. **Merge + initial polish — Codex.** Prompt: "Here are two independent specs for the same requirement. Produce one merged spec that takes the strongest parts of each, resolves contradictions in favor of the more concrete proposal, and adds anything either missed." Inputs: drafts A and B verbatim.
4. **Secondary refinement — Claude.** Prompt: "Here is a merged spec. Improve clarity, consistency, and completeness without changing scope or removing concrete decisions. Flag any unresolved ambiguity inline." Input: stage 3 output.

### Why this assignment of roles

Codex is the merger because cross-model synthesis is where the dual-AI value actually shows up — having a different model re-read both drafts catches things the original drafter wouldn't. Claude does the secondary refinement because it consistently produces tighter prose in our existing flows (Claude already owns brainstorm + spec-synth in `internal/cli/new.go`).

### Why parallel for stages 1–2

Halves wall-clock time on the most expensive part of the pipeline. Stages 3 and 4 must remain sequential — each depends on the previous output.

### Outputs on disk

- Final spec → `.aios/project.md` (overwrite — same path the rest of AIOS reads).
- Intermediate drafts and timing JSON → `.aios/runs/<run-id>/specgen/{draft-claude.md,draft-codex.md,merged.md,final.md,stages.json}`.
- The existing `aios cost` command picks these up automatically since it reads from the run dir.

## REPL session

A new file `internal/cli/repl.go`. The root command's `RunE` checks for no subcommand and no positional args; if so, enter REPL. Otherwise existing subcommand routing is untouched.

### Session shape

Each REPL session has a session ID (timestamp, like existing run IDs) and a single in-memory `Session` struct:

- Turn history — list of `{user message, final spec produced that turn, run ID}`.
- Current spec text.
- Run directory paths.

Persisted to `.aios/sessions/<session-id>/` after every turn so a crashed REPL can be resumed via `aios --resume` (no arg picks the most recent session) or `aios --resume <session-id>`.

### Turn loop

1. Read user input. Multi-line; blank line submits, matching Claude CLI conventions.
2. Detect slash commands first. If none, treat as natural language.
3. For natural-language input: call `specgen.Generate` with `{prior turns, current message, current spec if any}`. Show the quiet spinner. Write the new spec to `.aios/project.md` and to the session dir.
4. Print a one-line summary plus the available next-action prompts.
5. Loop.

### `/ship` handoff

Calls into the existing autopilot code path (`runAutopilot` in `internal/cli/autopilot.go`) with `--auto` so it does not re-prompt. The spec is already at `.aios/project.md`; autopilot picks it up, decomposes, runs tasks, opens the PR. The REPL exits when autopilot returns — both success and failure end the session, since autopilot already prints its own summary.

### Refinement context window

Each turn's input to `specgen.Generate` includes the full prior conversation plus the current spec. If the accumulated context exceeds a conservative byte threshold (200 KB of prior turns), older turns are summarized into a single "prior context" block before sending. Byte threshold beats token counting — simple, deterministic, no tokenizer dependency.

## Error handling

The pipeline has four model calls across two CLIs. Each can fail independently.

| Failure | Response |
|---|---|
| Stage 1 fails (Claude draft) | Skip merge. Run stage 4 (Claude polish) on Codex's draft. Warn: "Claude draft failed; spec built from Codex alone." |
| Stage 2 fails (Codex draft) | Skip merge. Run stage 4 (Claude polish) on Claude's draft. Warn: "Codex draft failed; spec built from Claude alone." |
| Both stage 1 and 2 fail | Hard error. Show both error messages. Leave prior spec intact. Return to REPL prompt. |
| Stage 3 fails (merge) | Fall back to whichever draft is longer (cheap heuristic for "more complete"). Run stage 4 on that. Warn. |
| Stage 4 fails (polish) | Save stage 3 output as final. Warn: "Polish step failed; spec is the merged version." |
| Codex CLI not installed | Refuse to launch REPL at startup. Point user to `aios doctor`. No partial-mode fallback — the dual-AI pipeline is the entire value proposition. |
| Claude CLI not installed | Same — refuse to launch. |
| REPL crashes mid-session | Session state is on disk after every turn. `aios --resume` restores last good state. |
| `/ship` fails | Autopilot prints its own error and exits with its own code. REPL exits with that code. |

**No automatic retries.** A failed model call stays failed. Burning two more API hits to maybe succeed obscures real problems and inflates cost. The user can re-submit the same turn manually if they want a retry.

## Testing

### Unit tests (table-driven, mocked engines)

- `internal/specgen/pipeline_test.go` — happy path, each single-stage failure (1, 2, 3, 4 individually), both-drafts-fail, parallel timing verification (fake engine records start times for stages 1 and 2).
- `internal/cli/repl_test.go` — slash-command dispatch, turn loop with mocked stdin/stdout, session state persistence after each turn, resume from on-disk session, refusal when either CLI is missing.
- `internal/cli/repl_session_test.go` — context-window summarization triggers at 200 KB, prior turns survive across turns, current spec is fed back into the next turn's prompt.

### Integration tests (existing fake-engine harness)

- End-to-end: launch REPL → submit one requirement → verify all four intermediate drafts on disk → verify final at `.aios/project.md` → submit `/ship` → verify autopilot fired with `--auto` → verify task files written.
- Failure replay: same flow with stage 3 wired to fail → verify fallback to longer draft → verify warning printed.

### Reuse, not new infrastructure

The fake engine in `internal/engine/` already supports per-call canned responses and failure injection (used by `internal/cli/run_autopilot_test.go`). New tests use the same harness — no new mocking layer.

### Out of scope for tests

- Real Claude/Codex CLI calls in CI (slow, flaky, costs money — covered by existing manual `test/e2e/`).
- Token-counting accuracy (sidestepped by using a byte threshold for context summarization).

## Files to create

- `internal/specgen/pipeline.go` — `Generate` and the four stage runners.
- `internal/specgen/pipeline_test.go` — unit tests.
- `internal/specgen/prompts/` — four prompt templates (draft, merge, polish — with the merge prompt taking two inputs).
- `internal/cli/repl.go` — REPL command, turn loop, slash-command dispatch.
- `internal/cli/repl_test.go` — REPL unit tests.
- `internal/cli/repl_session.go` — session struct, persistence, resume.
- `internal/cli/repl_session_test.go` — session unit tests.
- `internal/cli/repl_integration_test.go` — end-to-end with fake engines.

## Files to modify

- `internal/cli/root.go` — when invoked with no subcommand and no args, dispatch to REPL. Add `--resume` flag at the root level.
- `README.md` — new section documenting the interactive `aios` entry point and the pipeline behavior; note that `aios new` and `aios architect` remain available.
- `docs/architecture.md` — add the specgen pipeline to the system overview.

## Open questions deferred to implementation

None blocking. Two minor decisions that the implementation plan can resolve:

- Exact prompt text for each of the four stages (drafted in this design, finalized when wiring up the prompt templates).
- The 200 KB context-window threshold is a starting estimate; tunable in code without API changes.
