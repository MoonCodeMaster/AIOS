# Changelog

## v0.3.2 — CLI UX overhaul (Codex-style)

### New features

- feat(cli): `aios exec <prompt>` — run the full pipeline non-interactively,
  like `codex exec`. Supports `--json` for machine-readable JSONL output.
  Alias: `aios e`.
- feat(cli): `aios resume [session-id]` — list and resume previous REPL
  sessions. `--last` continues the most recent session without a picker.
  Replaces the old `--continue` flag as the primary resume mechanism.
- feat(cli): `aios completion <shell>` — generate shell completion scripts
  for bash, zsh, fish, and powershell.
- feat(cli): `--model/-m` flag overrides the coder engine at runtime without
  editing config.toml.
- feat(cli): `--quiet/-q` suppresses progress output (errors and final
  results only). `--verbose` enables debug-level output.
- feat(cli): `--no-color` disables colored output for pipes and CI.

### Improvements

- feat(cli): colored output everywhere — green ✓ for success, red ✗ for
  errors, yellow ⚠ for warnings, cyan for commands and prompts, dim for
  secondary text. Uses `fatih/color` with automatic TTY detection.
- feat(cli): ASCII art banner on the landing card with version display and
  quick-start commands.
- feat(cli): braille spinner animation (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏) in the stage ticker
  replaces the static `↻`. Elapsed times and separators are dimmed.
- feat(cli): REPL prompt upgraded from `> ` to colored `❯`, continuation
  prompt from `.. ` to `· `. Welcome banner shows version and session info.
- feat(cli): `--help` output uses bold section headers, cyan command names,
  and dim footer text. New commands appear in their respective groups.

### Migration

| Before | After |
|---|---|
| `aios --continue id` | `aios resume id` or `aios -c id` (both work) |
| `aios resume task-1` (v0.2 alias) | `aios unblock task-1` (unchanged) |

## v0.3.1 — REPL responsiveness & Ctrl+C

### Fixes

- fix(engine): wire `TimeoutSec` into a real `context.WithTimeout` deadline.
  Was configured (default 600s) but never enforced — a wedged `claude` or
  `codex` invocation hung the pipeline forever. Also sets
  `cmd.WaitDelay = 500ms` so descendants holding inherited stdio pipes
  don't keep `Wait()` blocked after the kill. Timeout now surfaces as
  `claude timed out after 600s — check 'aios doctor' …` instead of
  `signal: killed (stderr: )`.
- fix(cli): Ctrl+C exits the REPL. Previously `bufio.Scanner.Scan` blocked
  on stdin and ignored the cancelled context, so the user had to also press
  Enter or Ctrl+D. The signal handler now closes stdin to wake the scanner;
  a second Ctrl+C is a hard exit (130).
- fix(cli): no more spurious `aios: interrupt received, cancelling…` on
  every clean exit (`--version`, normal command completion). The line now
  fires only on a real signal.
- fix(cli): suppress `turn failed: context canceled` on Ctrl+C — the REPL
  exits silently on cancel.

### Improvements

- feat(cli): live status line during the dual-AI pipeline. In a TTY, a
  single redrawing line shows every active stage with elapsed time
  (`↻ draft-claude 23s · draft-codex 19s`), collapsing to permanent
  ✓/✗ summary lines as each stage finishes. Non-TTY (pipes, CI) keeps
  the old per-line output. Resolves the "stuck on draft-claude / codex
  not running" confusion — both drafts ran in parallel; nothing rendered
  their progress.
- feat(specgen): new `OnStageProgress(name, elapsed)` callback on
  `Input` and `RegenerateInput`, fired ~every 1s while a stage is in
  flight. Drives the live status UI; safe for concurrent stages.
- feat(cli): pre-pipeline banner (`Drafting spec with Claude + Codex in
  parallel — typically 30–90s.`) so users see what they're waiting for
  before any stage starts.

## v0.3.0 — CLI UX pass

### Breaking changes

- `aios --ship "prompt"` is removed. Use `aios ship "prompt"`.
- `aios resume <task>` renamed to `aios unblock <task>`.
- `--continue` is no longer a persistent flag — use `aios -c [id]` (root only).
- `--dry-run` and `--yolo` are no longer persistent — they only appear on `aios ship` and `aios run`.
- Bare `aios` outside an AIOS repo prints a friendly landing card and exits 0 (was: error + full help dump).
- `aios "prompt"` outside an AIOS repo errors with a one-line hint (was: error + full help dump).

### Improvements

- `--config <path>` now actually works (was silently ignored).
- All "no config" / "not in repo" errors come from one canonical message.
- Cobra usage dump is suppressed on errors — error messages stand alone.
- `--help` is grouped: Session / Pipeline / Setup / Inspection / Flags.
- `aios init` now hints at the next command (`aios doctor`).
- `aios doctor` runs in git-only repos (no longer needs `.aios/config.toml` first), so it can diagnose a pre-init machine.

### Migration

| Before | After |
|---|---|
| `aios --ship "x"` | `aios ship "x"` |
| `aios --continue id` | `aios -c id` or `aios --continue id` |
| `aios resume task-1` | `aios unblock task-1` |
| `aios run --dry-run` | unchanged (still works; flag now per-command) |
| `aios status --dry-run` | flag removed (it never did anything on `status`) |

## v0.2.1

- fix(cli): REPL submits on a single Enter and prints stage durations, so the
  prompt no longer appears frozen after typing. Multi-line input is opt-in via
  trailing `\` continuation or a `"""…"""` block. Banner and `/help` updated.
- fix(orchestrator): preserve task input order when enqueuing the initial
  ready set and dependents released by completion, so deterministic specs run
  in the order users wrote them.
- ci(release): make `publish-npm` idempotent — skip versions already on the
  registry and tolerate provenance fallback. Restrict the publish job to
  `v*` tag pushes; non-tag runs use goreleaser snapshot mode.

## v0.2.0

- feat(ship): regenerate spec when sibling tasks abandon with overlapping
  issues. When >=2 tasks abandon and their reviewer-issue fingerprints overlap
  (Jaccard >= 0.5), the spec is regenerated with failure feedback and the run
  retries once. Capped at one respec attempt per ship invocation. New config
  fields under `[budget]`: `respec_on_abandon` (default true),
  `respec_min_overlap_score` (default 0.5). Audit artifacts:
  `respec/feedback.md`, `respec/new-project.md`, `respec/old-tasks/`.

- feat(specgen): cross-model critique stage scores polished spec; one refine
  cycle when below threshold. New config section `[specgen]` with
  `critique_enabled` (default true) and `critique_threshold` (default 9,
  range 0-12). Critique runs on the engine NOT used for polish. Refine runs
  on the polish engine. Audit artifacts: `5-critique.md`, `5-score.json`,
  `6-refine.md`. Fast-path (score >= threshold) adds one engine call;
  refine-path adds two.

- feat(orchestrator): compress prior round history when chains exceed 2 rounds.
  New config fields under `[budget]`: `compress_history` (default true in
  v0.2.0), `compress_after_rounds` (default 2), `compress_target_tokens`
  (default 50000). Two strategies: algorithmic (default, no LLM call) and
  LLM (opt-in, uses reviewer engine). Compressed brief persisted to
  `compressed-prior.txt` per round.

- feat(engine): retry transient claude/codex failures (3 attempts, exponential
  backoff). New config fields on `[engines.claude]` and `[engines.codex]`:
  `retry_max_attempts`, `retry_base_ms`, `retry_enabled`. Defaults: 3
  attempts, 1s base, enabled. Failed attempts are recorded in
  `coder.attempts.json` / `reviewer.attempts.json` per round when retries
  occurred.

- **Behavior change:** `compress_history` default flips from `false` to
  `true`. Existing configs with explicit `false` are unaffected.

## v0.1.1

- Initial release.
