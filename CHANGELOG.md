# Changelog

## v0.2.0

- feat(ship): regenerate spec when sibling tasks abandon with overlapping
  issues. When >=2 tasks abandon and their reviewer-issue fingerprints overlap
  (Jaccard >= 0.5), the spec is regenerated with failure feedback and the run
  retries once. Capped at one respec attempt per ship invocation. New config
  fields: `respec_on_abandon` (default true), `respec_min_overlap_score`
  (default 0.5). Audit artifacts: `respec/feedback.md`,
  `respec/new-project.md`, `respec/old-tasks/`.

- feat(specgen): cross-model critique stage scores polished spec; one refine
  cycle when below threshold. New config section `[specgen]` with
  `critique_enabled` (default true) and `critique_threshold` (default 9).

- **Behavior change:** `compress_history` and `respec_on_abandon` defaults
  flip to `true`. Existing configs with explicit `false` are unaffected.

## v0.1.1

- Initial release.
