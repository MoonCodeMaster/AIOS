# `aios architect` — Multi-Round Mind-Map Front Door

> **Engineer brief:** REQUIRED SUB-SKILL — use `superpowers:executing-plans` to walk this plan task-by-task. Each step is independently testable.

**Goal:** Add a single command — `aios architect "<idea>"` — that runs Claude CLI and Codex CLI through a 4-round mutual-critique pipeline, presents the user three distinct, fully-formed mind-map blueprints, and on selection chains straight into the existing `aios autopilot` flow without any further human input.

**Why now:** Today AIOS shines at *executing* a spec. It does not yet help the user *frame* what to build. Raw `claude` and `codex` give one model's first answer; AIOS can give three structurally-different blueprints that each model has stress-tested. That difference is the strongest pitch for picking AIOS over either CLI alone.

**Architecture:** A new `internal/architect` package owns the pipeline (independent generation → cross-critique → refinement → synthesis). A new `internal/cli/architect.go` Cobra command wires the pipeline to the engines, prints the finalists, takes a selection, then re-uses `runNew` + `runMain` (autopilot+merge) so the rest of the lifecycle is unchanged.

**Tech stack:** Go 1.26, Cobra, existing `engine.Engine` abstraction, embedded `text/template` prompts. No new external deps.

---

## File map

**New:**
- `internal/architect/architect.go` — `Blueprint` struct, `Run` entry point.
- `internal/architect/parse.go` — parse model output into `[]Blueprint`.
- `internal/architect/render.go` — render `Blueprint` as ASCII mind map + markdown.
- `internal/architect/architect_test.go`, `parse_test.go`, `render_test.go`.
- `internal/cli/architect.go` — `newArchitectCmd()`, selection prompt, autopilot chain.
- `internal/cli/architect_test.go`.
- `internal/engine/prompts/bp-propose.tmpl`
- `internal/engine/prompts/bp-critique.tmpl`
- `internal/engine/prompts/bp-refine.tmpl`
- `internal/engine/prompts/bp-synthesize.tmpl`
- `internal/engine/prompts/bp-to-spec.tmpl`
- `docs/superpowers/plans/2026-04-26-architect-mindmap.md` (this file)

**Modified:**
- `internal/cli/root.go` — register `architect` subcommand.
- `README.md` — document the new front door.
- `docs/architecture.md` — add the architect pipeline diagram.

---

## Pipeline (in plain words)

1. **Round 1 — Independent vision.** Claude generates blueprints A and B. Codex generates blueprint C. Three independent first answers, fully in parallel.
2. **Round 2 — Cross-critique.** Codex critiques A and B; Claude critiques C. Each critique is a JSON-ish bullet list: `gaps`, `risks`, `novelty_score`, `suggested_amendments`.
3. **Round 3 — Refinement.** Each author rewrites its own blueprint(s) using the other model's critique as input. Same blueprint count out: A', B', C'.
4. **Round 4 — Synthesis.** The reviewer-default engine takes A', B', C' and emits exactly three finalists labelled "Conservative", "Balanced", "Ambitious". The synthesizer is allowed to merge or split, but the constraint is that each finalist must be a complete, internally-consistent mind map.
5. **Selection.** CLI prints the three mind maps to stdout, then asks `Pick blueprint [1/2/3]`. Flags `--pick N` and `--auto` (= `--pick 1`) skip the prompt.
6. **Auto-execution.** Selected blueprint → `bp-to-spec` template → `project.md` → existing `runNew(opts.Auto=true)` writes tasks → `runMain` runs in autopilot+merge mode. Zero further prompts.

---

## Blueprint format

Each blueprint is plaintext markdown bracketed by `===BLUEPRINT===` markers, with a strict shape:

```
===BLUEPRINT===
title: <one short line>
tagline: <one sentence>
stance: conservative | balanced | ambitious

## Mind map
- root: <project name>
  - subsystem A
    - module a1
    - module a2
  - subsystem B
    - module b1
  ...

## Architecture sketch
5–10 lines.

## Data flow
1. ...
2. ...

## Tradeoffs
- pro: ...
- con: ...

## Roadmap
- M1 ...
- M2 ...

## Risks
- risk: <name> | mitigation: <plan>
===END===
```

The parser reads everything between `===BLUEPRINT===` and `===END===`, splits on `## ` headings, and pulls out the seven fields. A blueprint missing `title`, `stance`, or `## Mind map` is dropped (with a warning) — the synthesizer round must produce three valid ones.

---

## Tasks

### Task 1 — Plan document

**Files:** Create: `docs/superpowers/plans/2026-04-26-architect-mindmap.md` (this file).

- [x] Step 1: Write plan. (You're reading it.)

### Task 2 — Prompt templates

**Files:** Create: `internal/engine/prompts/bp-propose.tmpl`, `bp-critique.tmpl`, `bp-refine.tmpl`, `bp-synthesize.tmpl`, `bp-to-spec.tmpl`.

- [ ] Step 1: Write the five `.tmpl` files. Templates use the embedded FS already wired in `prompts.go`, so no Go changes needed.
- [ ] Step 2: Run `go test ./internal/engine/prompts/...` — tmpl parse must still pass.

### Task 3 — `internal/architect` types + parser

**Files:** Create: `internal/architect/architect.go`, `parse.go`, `parse_test.go`, `render.go`, `render_test.go`.

- [ ] Step 1: Define `Blueprint` struct with `Title, Tagline, Stance, MindMap, Sketch, DataFlow, Tradeoffs, Roadmap, Risks` fields.
- [ ] Step 2: Write failing `TestParse_ThreeBlueprints` — feeds a fixture with three valid blocks, expects three structs.
- [ ] Step 3: Implement `ParseBlueprints(raw string) []Blueprint`. Tolerant: drops blocks missing required fields rather than erroring (synthesis path expects 3, but parser is reused upstream where partials are normal).
- [ ] Step 4: Write `TestRender_RoundTrip` — render then re-parse must yield equal struct.
- [ ] Step 5: Implement `Render(b Blueprint) string` matching the documented shape exactly.
- [ ] Step 6: `go test ./internal/architect/...` green.

### Task 4 — 4-round pipeline

**Files:** Create: `internal/architect/pipeline.go`, `pipeline_test.go`.

- [ ] Step 1: Define `Input{Idea, Claude, Codex, Synthesizer engine.Engine}` and `Output{Finalists []Blueprint, RawArtifacts map[string]string}`.
- [ ] Step 2: Write `TestRun_HappyPath_ThreeFinalists` using `engine.FakeEngine` scripted to return canned blueprints/critiques/syntheses; asserts 3 finalists.
- [ ] Step 3: Implement `Run(ctx, in)`. Round 1 fan-out parallel via `sync.WaitGroup`; Round 2 fan-out parallel; Round 3 sequential per author; Round 4 single call to synthesizer. Each round renders its prompt via the embedded templates.
- [ ] Step 4: Write `TestRun_FallbackOnSynthesizerError` — when synthesis errors, fall back to the three refined blueprints.
- [ ] Step 5: Write `TestRun_RequiresThreeFinalists` — when synthesis returns fewer than 3 parseable blueprints, return ErrSynthesisShort.
- [ ] Step 6: Implement those two paths, all tests green.

### Task 5 — `aios architect` CLI command

**Files:** Create: `internal/cli/architect.go`, `internal/cli/architect_test.go`. Modify: `internal/cli/root.go`.

- [ ] Step 1: Define `newArchitectCmd()` with flags `--pick int` (1..3) and `--auto` (alias for `--pick 1`).
- [ ] Step 2: Construct Claude+Codex engines from config (mirror `runNew`).
- [ ] Step 3: Call `architect.Run`, persist raw artifacts under `.aios/runs/<id>/architect/` for audit (round-N/<author>.txt).
- [ ] Step 4: Render the three finalists to stdout. If `--auto` or `--pick`, skip the prompt; else read `[1/2/3]` from stdin (re-use existing `confirm`-style helper or write a tiny `pickOne`).
- [ ] Step 5: Render `bp-to-spec` with the chosen blueprint, write to `.aios/project.md`, then build a synthetic decompose request — but the cleanest reuse is to call the existing `runNew` machinery with the blueprint converted to a spec-shaped idea. We do this by writing project.md ourselves and a synthetic transcript, then calling decompose directly via the existing prompt path.
- [ ] Step 6: After spec+tasks land, fabricate the same flag wiring as `newAutopilotCmd` (`--autopilot`, `--merge`) and call `runMain`.
- [ ] Step 7: Register in `root.go`.

### Task 6 — Tests, build, smoke

- [ ] Step 1: `go build ./...`
- [ ] Step 2: `go test ./...` (allow the known-flaky `TestRebaseReviewRejects` one retry).
- [ ] Step 3: Confirm `aios architect --help` renders.

### Task 7 — Docs

**Files:** Modify: `README.md` (add "Architect" section near the top); `docs/architecture.md` (add pipeline section).

- [ ] Step 1: README: short pitch + example invocation.
- [ ] Step 2: docs/architecture.md: extend the pipeline diagram and explain the 4-round flow.

### Task 8 — Self-review + commit

- [ ] Step 1: `git diff` read-through.
- [ ] Step 2: Commit on `feat/architect-mindmap`. Conventional-commit style, no AI trailer (per project preference).
