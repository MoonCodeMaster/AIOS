# Changelog

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
