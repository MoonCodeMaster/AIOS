# Changelog

## v0.1.3

- feat(orchestrator): compress prior round history when chains exceed 2 rounds (opt-in).
  New config fields under `[budget]`: `compress_history` (default false),
  `compress_after_rounds` (default 2), `compress_target_tokens` (default 50000).
  Two strategies: algorithmic (default, no LLM call) and LLM (opt-in, uses
  reviewer engine). Compressed brief persisted to `compressed-prior.txt` per
  round. Default flips to true in v0.2.0.

## v0.1.2

- feat(engine): retry transient claude/codex failures (3 attempts, exponential backoff).
  New config fields on `[engines.claude]` and `[engines.codex]`: `retry_max_attempts`,
  `retry_base_ms`, `retry_enabled`. Defaults: 3 attempts, 1s base, enabled.
  Failed attempts are recorded in `coder.attempts.json` / `reviewer.attempts.json`
  per round when retries occurred.

## v0.1.1

- Initial release.
