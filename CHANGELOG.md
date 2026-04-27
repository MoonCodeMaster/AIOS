# Changelog

## v0.1.2

- feat(engine): retry transient claude/codex failures (3 attempts, exponential backoff).
  New config fields on `[engines.claude]` and `[engines.codex]`: `retry_max_attempts`,
  `retry_base_ms`, `retry_enabled`. Defaults: 3 attempts, 1s base, enabled.
  Failed attempts are recorded in `coder.attempts.json` / `reviewer.attempts.json`
  per round when retries occurred.

## v0.1.1

- Initial release.
