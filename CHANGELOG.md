# Changelog

## v0.1.3

- feat(orchestrator): compress prior round history when chains exceed 2 rounds (opt-in).
  New config fields under `[budget]`: `compress_history` (default false),
  `compress_after_rounds` (default 2), `compress_target_tokens` (default 50000).
  Two strategies: algorithmic (default, no LLM call) and LLM (opt-in, uses
  reviewer engine). Compressed brief persisted to `compressed-prior.txt` per
  round. Default flips to true in v0.2.0.

## v0.1.1

- Initial release.
