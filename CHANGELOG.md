# Changelog

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
