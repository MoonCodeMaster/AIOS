# Architect → Internal Complex-Task Planner — Implementation Plan (Plan 2 of 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Repackage `internal/architect/`'s divergent-planning + critique + synthesis logic so it stops being a deleted-but-still-resident user-facing package and becomes an internal complexity-triggered pre-stage of `specgen.Generate`. When the user submits a complex prompt, AIOS now runs propose → critique → refine → synthesize-to-one to produce a high-quality blueprint, then feeds the blueprint into the existing 4-stage specgen drafts. Single document out, no 3-finalist UX, no user-facing "architect" command.

**Non-goals (deferred to Plan 3):**
- Repo-context ingestion (reading the codebase before planning).
- Spec quality gate (a model reviews the polished spec before decomposition).
- Intake stage (asking only blocking questions, writing assumptions for the rest).
- Adaptive re-planning for stuck/oversized tasks.

**Architecture:** `internal/architect/` keeps its package name and most of its rendering/parsing helpers. The user-visible `Run(ctx, in) (Output, error)` (3-finalist) is replaced by `Plan(ctx, in) (Blueprint, error)` (single blueprint). A new `internal/specgen/complexity.go` owns the trigger heuristic. `specgen.Generate` consults the heuristic on entry; when complex, it calls `architect.Plan` and threads the resulting Blueprint into the draft prompt template via a new `{{if .Blueprint}}` section. Simple prompts skip the planner and run the existing 4-stage pipeline unchanged.

**Tech Stack:** Go 1.21, existing engine/specgen packages, existing `bp-*.tmpl` prompt templates (modified — `bp-synthesize.tmpl` now produces ONE blueprint, not three).

**Spec / direction:** This conversation thread (Plan 2 scope locked in the user's earlier "four-plan split" confirmation, with the user's explicit guidance: "preserve architect-style multi-plan critique internally for complex tasks").

---

## Design decisions baked into this plan

These are the decisions I'd otherwise need to brainstorm separately. Stated up-front so the plan reads cleanly. **If any of these is wrong, redirect before execution.**

1. **Trigger heuristic.** Single rule, no model classification call: prompt is "complex" if `len(prompt) > 200` chars OR it contains any of these keywords (case-insensitive): `system`, `architecture`, `platform`, `subsystem`, `pipeline`, `infrastructure`, `multi-`, `several`, `migration`, `redesign`, `refactor large`. Constants `complexityCharThreshold` and `complexityKeywords` in `internal/specgen/complexity.go` — tunable in code.

2. **Output unification.** Architect now returns ONE Blueprint, not 3 finalists. Internally still 4 rounds (propose → critique → refine → synthesize), but the final synthesizer prompt asks for one best blueprint, not three labeled options.

3. **Integration point.** New optional field `specgen.Input.Blueprint *architect.Blueprint`. When non-nil, the draft template renders the blueprint as a "Reference blueprint" section. `specgen.Generate` checks complexity at entry; if complex, runs `architect.Plan` to populate this field before starting the 4 stages.

4. **Old `Run` deletion.** No callers remain after Plan 1 deleted the CLI wrapper. Deletes are safe.

5. **`bp-synthesize.tmpl` rewrite.** Single blueprint output, not 3. The old "conservative/balanced/ambitious" framing goes away. Synthesizer is asked to produce one best-of-both-worlds blueprint.

6. **Architect's parallel goroutine structure stays.** Cross-model critique is the value proposition; I'm not rewriting the rounds.

7. **Audit trail.** Architect artifacts persist under the same `.aios/runs/<run-id>/specgen/` directory used by specgen, in a new `architect/` subfolder. One persistence path, not two.

8. **No new flags.** No user-facing `--complex` opt-in or `--no-complex` opt-out. Heuristic is internal. (Plan 3 may add `--simple` if users push back.)

---

## Task 1: Add complexity heuristic

**Files:**
- Create: `internal/specgen/complexity.go`
- Create: `internal/specgen/complexity_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/specgen/complexity_test.go`:

```go
package specgen

import "testing"

func TestIsComplexPrompt(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"short-no-keywords", "add a /health endpoint", false},
		{"short-with-system-keyword", "redesign auth system", true},
		{"short-with-architecture-keyword", "draft new architecture", true},
		{"short-with-platform-keyword", "build a platform", true},
		{"short-with-subsystem-keyword", "extract subsystem", true},
		{"short-with-pipeline-keyword", "build a pipeline for events", true},
		{"short-with-infrastructure-keyword", "migrate infrastructure", true},
		{"short-with-multi-prefix", "build a multi-tenant store", true},
		{"short-with-several-keyword", "fix several bugs", true},
		{"short-with-migration-keyword", "do a migration", true},
		{"short-with-redesign-keyword", "redesign the inbox", true},
		{"long-no-keywords", longPrompt201Chars(), true},
		{"borderline-200-chars-no-keywords", longPrompt200Chars(), false},
		{"keyword-case-insensitive", "Refactor large module", true},
		{"empty", "", false},
		{"keyword-substring-only-rejected", "submarine maintenance", false}, // 'mar' is in 'platform' but we only match whole words
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isComplexPrompt(c.in)
			if got != c.want {
				t.Fatalf("isComplexPrompt(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func longPrompt201Chars() string {
	out := ""
	for i := 0; i < 201; i++ {
		out += "x"
	}
	return out
}

func longPrompt200Chars() string {
	out := ""
	for i := 0; i < 200; i++ {
		out += "x"
	}
	return out
}
```

- [ ] **Step 2: Verify test fails**

`go test ./internal/specgen/ -run TestIsComplexPrompt` — expect compile failure (`isComplexPrompt` undefined).

- [ ] **Step 3: Create `internal/specgen/complexity.go`**

```go
package specgen

import (
	"regexp"
	"strings"
)

const complexityCharThreshold = 200

// complexityKeywords are whole-word triggers (case-insensitive). Each is
// compiled into a word-boundary regex so "submarine" won't match "marine"
// or "system" inside "subsystems".
var complexityKeywords = []string{
	"system",
	"architecture",
	"platform",
	"subsystem",
	"pipeline",
	"infrastructure",
	"multi-",
	"several",
	"migration",
	"redesign",
	"refactor large",
}

var complexityRegex = func() *regexp.Regexp {
	parts := make([]string, 0, len(complexityKeywords))
	for _, k := range complexityKeywords {
		parts = append(parts, `\b`+regexp.QuoteMeta(k)+`\b`)
	}
	return regexp.MustCompile("(?i)(" + strings.Join(parts, "|") + ")")
}()

// isComplexPrompt returns true when the user's request looks like a
// multi-system / architectural task that benefits from the architect
// pre-stage. Single rule: length > complexityCharThreshold OR a keyword
// match. Tunable via the constants above.
func isComplexPrompt(prompt string) bool {
	if len(prompt) > complexityCharThreshold {
		return true
	}
	return complexityRegex.MatchString(prompt)
}
```

- [ ] **Step 4: Run tests**

`go test ./internal/specgen/ -run TestIsComplexPrompt -v` — all 16 cases pass.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/complexity.go internal/specgen/complexity_test.go
git commit -m "feat(specgen): complexity heuristic — length + keyword triggers"
```

---

## Task 2: Add `architect.Plan` (single-blueprint synthesis)

**Files:**
- Modify: `internal/architect/pipeline.go` (add new `Plan` function alongside existing `Run`; do not delete `Run` yet — Task 5 does that)
- Modify: `internal/engine/prompts/bp-synthesize.tmpl` — adapt to optional single-output mode
- Create: `internal/engine/prompts/bp-synthesize-one.tmpl` — new prompt asking for ONE best blueprint
- Modify: `internal/architect/pipeline_test.go` — add `TestPlanReturnsOneBlueprint`

**Decision:** Add a new `bp-synthesize-one.tmpl` rather than parameterising the existing one. Two reasons: (1) the existing prompt has the "conservative/balanced/ambitious" framing baked in, which we don't want for the single output; (2) keeping the templates side-by-side makes the deletion in Task 5 a clean file removal.

- [ ] **Step 1: Create the new synthesis prompt**

Create `internal/engine/prompts/bp-synthesize-one.tmpl`:

```
You are synthesising the best blueprint from a pool of refined proposals
for the same idea. Choose the strongest concrete decisions from each,
resolve contradictions in favour of the more specific proposal, and add
anything either missed.

Output ONE blueprint with the following sections, in this exact order:

Title: <short noun phrase>
Tagline: <one sentence>
Stance: <one of: conservative | balanced | ambitious — pick the one that
best fits the synthesised result>
MindMap:
- root: <root concept>
  - <child>
    - <leaf>
  ...
Sketch:
<2-5 line architecture sketch — components and their relationships>
DataFlow:
1. <step>
2. <step>
...
Tradeoff:
Pros:
- <pro>
Cons:
- <con>
Roadmap:
- M1: <milestone>
- M2: <milestone>
Risks:
- <risk> → <mitigation>

Idea:
{{.Idea}}

Refined pool:
{{.Pool}}

Output ONLY the blueprint. No preamble, no commentary.
```

- [ ] **Step 2: Write the failing test**

Append to `internal/architect/pipeline_test.go`:

```go
func TestPlanReturnsOneBlueprint(t *testing.T) {
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: validBlueprintFixture("c1") + "\n---\n" + validBlueprintFixture("c2")}, // round 1: 2 proposals
		{Text: "critique-of-codex"},  // round 2: claude critiques codex
		{Text: validBlueprintFixture("c1-refined") + "\n---\n" + validBlueprintFixture("c2-refined")}, // round 3: refine
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: validBlueprintFixture("x1")}, // round 1: 1 proposal
		{Text: "critique-of-claude"}, // round 2: codex critiques claude
		{Text: validBlueprintFixture("x1-refined")}, // round 3: refine
	}}
	synth := &engine.FakeEngine{Name_: "synth", Script: []engine.InvokeResponse{
		{Text: validBlueprintFixture("FINAL")}, // round 4: synthesise to ONE
	}}
	out, err := Plan(context.Background(), Input{
		Idea: "x", Claude: claude, Codex: codex, Synthesizer: synth,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if out.Title != "FINAL" {
		t.Fatalf("Title = %q, want FINAL", out.Title)
	}
	if !out.Valid() {
		t.Fatalf("returned blueprint is not Valid: %+v", out)
	}
}

// validBlueprintFixture returns a string that the parser will accept as
// one Blueprint (Title/Stance/MindMap minimum).
func validBlueprintFixture(title string) string {
	return "Title: " + title + "\nTagline: t\nStance: balanced\nMindMap:\n- root: x\nSketch:\ns\nDataFlow:\n1. d\nTradeoff:\nPros:\n- p\nCons:\n- c\nRoadmap:\n- M1: m\nRisks:\n- r → m\n"
}
```

(`context` and `engine` are already imported from existing tests.)

- [ ] **Step 3: Verify test fails**

`go test ./internal/architect/ -run TestPlanReturnsOne` — expect compile failure (`Plan` undefined).

- [ ] **Step 4: Add `Plan` to `internal/architect/pipeline.go`**

Append to `pipeline.go`:

```go
// Plan runs the same 4-round divergent-planning pipeline as Run, but the
// final synthesis stage produces ONE blueprint instead of three finalists.
// Used by specgen.Generate when the prompt is detected as complex; not a
// user-facing entry point.
//
// Inputs and audit-trail format match Run; the only difference is the
// final output (single Blueprint vs three).
func Plan(ctx context.Context, in Input) (Blueprint, error) {
	// Reuse the rounds-1-through-3 logic by extracting it into a helper.
	pool, raws, err := runRoundsToRefined(ctx, in)
	if err != nil {
		return Blueprint{}, err
	}
	// Round 4 (single-blueprint synthesis).
	synthPrompt, err := prompts.Render("bp-synthesize-one.tmpl", map[string]string{
		"Idea": in.Idea,
		"Pool": pool,
	})
	if err != nil {
		return Blueprint{}, fmt.Errorf("render bp-synthesize-one: %w", err)
	}
	r, err := in.Synthesizer.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: synthPrompt})
	if err != nil {
		return Blueprint{}, fmt.Errorf("synthesise: %w", err)
	}
	// Persist round-4 raw artifact alongside the rest.
	if raws != nil {
		raws["4-synth-one.txt"] = r.Text
	}
	bps := Parse(r.Text)
	if len(bps) == 0 || !bps[0].Valid() {
		return Blueprint{}, fmt.Errorf("synthesise: no valid blueprint in response: %s", r.Text)
	}
	return bps[0], nil
}

// runRoundsToRefined extracts rounds 1-3 from Run so Plan can reuse them.
// Returns the joined refined pool ready for synthesis, plus the raw-artifact
// map so the caller can extend it with round-4 output.
func runRoundsToRefined(ctx context.Context, in Input) (string, map[string]string, error) {
	// Body lifted verbatim from Run lines 53-225 (rounds 1-3) — extracted
	// so both Run and Plan can share the same divergent-planning code path.
	// Returns: pool (the refined-blueprints joined string), raws (audit trail).
	// On both-proposers-failed: returns the wrapped error.
	// ... (implementer: copy the existing rounds 1-3 body from Run, return
	//     pool string + RawArtifacts map; do NOT call the synthesis stage.)
	panic("implementer: extract from Run lines 53-225")
}
```

**Implementer note:** The cleanest way to do this is:
1. Open `internal/architect/pipeline.go`.
2. Identify where the existing `Run` ends rounds 1-3 and starts the synthesis stage (look for the comment "Round 4: synthesis").
3. Copy the rounds 1-3 body into a new function `runRoundsToRefined(ctx, in) (string, map[string]string, error)` that returns the joined refined pool and the raw-artifact map.
4. Modify `Run` to call `runRoundsToRefined` then do its own synthesis (preserving 3-finalist behaviour for now; Task 5 deletes Run anyway).
5. Implement `Plan` as the snippet above (calls `runRoundsToRefined` then the new single-blueprint synth prompt).

This keeps `Run` working through Tasks 1-4 (so existing architect tests stay green), and Task 5 deletes it cleanly.

- [ ] **Step 5: Run tests**

`go test -race ./internal/architect/ -v` — `TestPlanReturnsOneBlueprint` plus existing tests pass.
`go test -race ./...` — no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/architect/pipeline.go internal/architect/pipeline_test.go internal/engine/prompts/bp-synthesize-one.tmpl
git commit -m "feat(architect): Plan — single-blueprint synthesis for internal use"
```

---

## Task 3: Render a Blueprint into the draft prompt

**Files:**
- Modify: `internal/specgen/types.go` (add `Blueprint *architect.Blueprint` field to `Input`)
- Modify: `internal/specgen/prompts/draft.tmpl` (add `{{if .Blueprint}}…{{end}}` section)
- Modify: `internal/specgen/pipeline.go` (pass blueprint into the template render call)
- Modify: `internal/specgen/pipeline_test.go` (add `TestGenerateUsesBlueprintInDraftPrompt`)

- [ ] **Step 1: Write the failing test**

Append to `internal/specgen/pipeline_test.go`:

```go
import (
	"github.com/MoonCodeMaster/AIOS/internal/architect"
	// ... existing imports
)

func TestGenerateUsesBlueprintInDraftPrompt(t *testing.T) {
	bp := &architect.Blueprint{
		Title: "TEST_BLUEPRINT_TITLE", Stance: "balanced", MindMap: "- root: x",
		Sketch: "TEST_SKETCH_BODY", DataFlow: "1. step",
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	_, err := Generate(context.Background(), Input{
		UserRequest: "x", Blueprint: bp, Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	stage1Prompt := claude.Received[0].Prompt
	for _, want := range []string{"TEST_BLUEPRINT_TITLE", "TEST_SKETCH_BODY"} {
		if !strings.Contains(stage1Prompt, want) {
			t.Fatalf("draft prompt missing %q; got first 400 chars: %s", want, stage1Prompt[:400])
		}
	}
}
```

- [ ] **Step 2: Verify test fails**

`go test ./internal/specgen/ -run TestGenerateUsesBlueprint` — expect compile failure (`Input.Blueprint` undefined).

- [ ] **Step 3: Add the field to `internal/specgen/types.go`**

Insert into the `Input` struct (alongside existing fields):

```go
import "github.com/MoonCodeMaster/AIOS/internal/architect"
// ... existing imports

type Input struct {
    // ... existing fields ...
    // Blueprint, when non-nil, is rendered into the draft prompt as a
    // "Reference blueprint" section. Populated by Generate's internal
    // complex-prompt detection; callers should leave it nil.
    Blueprint *architect.Blueprint
}
```

- [ ] **Step 4: Modify `internal/specgen/prompts/draft.tmpl`**

Add a new optional section between the existing `{{if .ProjectContext}}` block and the closing `Output ONLY the spec.` line:

```
{{if .Blueprint}}

Reference blueprint (architect-mode pre-pass):
Title: {{.Blueprint.Title}}
Tagline: {{.Blueprint.Tagline}}
Stance: {{.Blueprint.Stance}}
MindMap:
{{.Blueprint.MindMap}}
Sketch:
{{.Blueprint.Sketch}}
DataFlow:
{{.Blueprint.DataFlow}}
Tradeoff:
{{.Blueprint.Tradeoff}}
Roadmap:
{{.Blueprint.Roadmap}}
Risks:
{{.Blueprint.Risks}}

Treat the blueprint as a strong starting point but feel free to deviate
where the spec needs more detail or a different decision.
{{end}}
```

- [ ] **Step 5: Pass the blueprint into the template render in `internal/specgen/pipeline.go`**

In `Generate`, find the `prompts.Render("draft.tmpl", ...)` call and add the blueprint to the data map:

```go
draftPrompt, err := prompts.Render("draft.tmpl", map[string]any{
    "UserRequest":    in.UserRequest,
    "CurrentSpec":    in.CurrentSpec,
    "PriorTurns":     priorForTmpl,
    "ProjectContext": in.ProjectContext,
    "Blueprint":      in.Blueprint, // nil when no architect pre-pass ran
})
```

- [ ] **Step 6: Run tests**

`go test -race ./internal/specgen/ -v` — new test passes; all existing specgen tests still pass (they pass nil Blueprint, the `{{if}}` block stays unrendered).
`go test -race ./...` — no regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/specgen/types.go internal/specgen/prompts/draft.tmpl internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): draft prompt accepts optional Blueprint reference"
```

---

## Task 4: Wire complexity detection into `specgen.Generate`

**Files:**
- Modify: `internal/specgen/pipeline.go` (add the architect pre-stage; thread Synthesizer engine through `Input`)
- Modify: `internal/specgen/types.go` (add `Synthesizer engine.Engine` field — defaults to Claude)
- Modify: `internal/specgen/pipeline_test.go` (add `TestGenerateRunsArchitectOnComplexPrompt`)

- [ ] **Step 1: Add `Synthesizer` to Input**

Edit `types.go`:

```go
type Input struct {
    // ... existing fields ...
    // Synthesizer is the engine used for the architect-mode synthesis
    // round when the prompt is complex. Defaults to Claude if nil.
    Synthesizer engine.Engine
}
```

- [ ] **Step 2: Write the failing test**

Append to `internal/specgen/pipeline_test.go`:

```go
func TestGenerateRunsArchitectOnComplexPrompt(t *testing.T) {
	// Complex prompt = matches a keyword.
	complex := "redesign the auth system"

	// Architect pipeline calls (claude=2 round-1 calls, codex=1 round-1, plus critique+refine).
	// We use the FakeEngine's script to feed a stub blueprint at the synthesise stage.
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		// Architect round 1 (Claude proposes 2)
		{Text: validBlueprintFixtureCli("c1") + "\n---\n" + validBlueprintFixtureCli("c2")},
		// Architect round 2 (Claude critiques Codex's)
		{Text: "critique-of-codex"},
		// Architect round 3 (Claude refines its own)
		{Text: validBlueprintFixtureCli("c1-refined") + "\n---\n" + validBlueprintFixtureCli("c2-refined")},
		// Specgen stage 1 (Claude draft)
		{Text: "DRAFT_A_uses_blueprint"},
		// Specgen stage 4 (Claude polish)
		{Text: "POLISHED_FINAL"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		// Architect round 1 (Codex proposes 1)
		{Text: validBlueprintFixtureCli("x1")},
		// Architect round 2 (Codex critiques Claude's)
		{Text: "critique-of-claude"},
		// Architect round 3 (Codex refines its own)
		{Text: validBlueprintFixtureCli("x1-refined")},
		// Specgen stage 2 (Codex draft)
		{Text: "DRAFT_B_uses_blueprint"},
		// Specgen stage 3 (Codex merge)
		{Text: "MERGED"},
		// Architect synthesis-one (Codex is default Synthesizer when nil and prompt prefers cross-model — but in this test we use claude as default)
	}}
	// In this test Synthesizer = claude (default). Claude must script a 6th response for synthesis.
	// Reorder: insert the synthesise call between round 3 and the specgen stages.
	// Easier: explicitly provide Synthesizer = a separate engine.
	synth := &engine.FakeEngine{Name_: "synth", Script: []engine.InvokeResponse{
		{Text: validBlueprintFixtureCli("SYNTHESISED")},
	}}
	out, err := Generate(context.Background(), Input{
		UserRequest: complex, Claude: claude, Codex: codex, Synthesizer: synth,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED_FINAL" {
		t.Fatalf("Final = %q", out.Final)
	}
	// The architect was used; the draft prompt for Claude (round 1 of specgen)
	// should contain the synthesised blueprint title.
	specgenStage1Prompt := claude.Received[3].Prompt // index 3 = first specgen call after 3 architect calls
	if !strings.Contains(specgenStage1Prompt, "SYNTHESISED") {
		t.Fatalf("specgen draft prompt missing synthesised blueprint; got first 600 chars: %s", specgenStage1Prompt[:600])
	}
}

func TestGenerateSkipsArchitectOnSimplePrompt(t *testing.T) {
	simple := "fix typo"
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	out, err := Generate(context.Background(), Input{
		UserRequest: simple, Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED" {
		t.Fatalf("Final = %q", out.Final)
	}
	// Architect did NOT run — claude has exactly 2 calls (drafts + polish), not 5.
	if len(claude.Received) != 2 {
		t.Fatalf("claude received %d calls, want 2 (architect should have been skipped)", len(claude.Received))
	}
}

func validBlueprintFixtureCli(title string) string {
	return "Title: " + title + "\nTagline: t\nStance: balanced\nMindMap:\n- root: x\nSketch:\ns\nDataFlow:\n1. d\nTradeoff:\nPros:\n- p\nCons:\n- c\nRoadmap:\n- M1: m\nRisks:\n- r → m\n"
}
```

- [ ] **Step 3: Verify tests fail**

`go test ./internal/specgen/ -run "TestGenerateRuns|TestGenerateSkips"` — expect failures (architect not yet wired).

- [ ] **Step 4: Wire the architect pre-stage in `internal/specgen/pipeline.go`**

At the top of `Generate` (after the nil-engine check, before the draft prompt render), add:

```go
	// Complex-prompt detection: run the architect pre-stage when the
	// prompt looks architectural. Skip for simple prompts to avoid the
	// 4-round latency penalty.
	if in.Blueprint == nil && isComplexPrompt(in.UserRequest) {
		synth := in.Synthesizer
		if synth == nil {
			synth = in.Claude // default
		}
		bp, err := architect.Plan(ctx, architect.Input{
			Idea:        in.UserRequest,
			Claude:      in.Claude,
			Codex:       in.Codex,
			Synthesizer: synth,
		})
		if err != nil {
			// Architect failure is non-fatal — fall back to plain specgen
			// without a blueprint. Surface as a warning.
			out.Warnings = append(out.Warnings, fmt.Sprintf("architect pre-stage failed; running plain specgen. (%s)", err))
		} else {
			in.Blueprint = &bp
			if in.OnStageStart != nil {
				in.OnStageStart("architect-complete")
			}
		}
	}
```

Add `"github.com/MoonCodeMaster/AIOS/internal/architect"` to the imports.

- [ ] **Step 5: Run all specgen tests**

`go test -race ./internal/specgen/ -v` — both new tests pass; all existing tests pass (simple-prompt path is unchanged).
`go test -race ./...` — no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/specgen/types.go internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): architect pre-stage runs automatically for complex prompts"
```

---

## Task 5: Delete the obsolete `architect.Run` (3-finalist)

**Files:**
- Modify: `internal/architect/pipeline.go` (delete `Run`)
- Delete: `internal/engine/prompts/bp-synthesize.tmpl` (replaced by `bp-synthesize-one.tmpl`)
- Modify: `internal/architect/pipeline_test.go` (delete tests of `Run`)
- Modify: `internal/architect/render.go` and `internal/architect/render_test.go` (delete render functions only used by `Run` output — likely the multi-blueprint user-facing render)

- [ ] **Step 1: Identify what's only used by `Run`**

`grep -rn "Run\|RenderForUser\|Output{" internal/architect/` — find the surface area unique to `Run`. Also check `internal/cli/` — Plan 1 deleted `architect.go` from `internal/cli/`, so there should be NO callers of `Run` outside the architect package.

- [ ] **Step 2: Delete `Run` and its types**

In `internal/architect/pipeline.go`:
- Delete the `Run` function entirely.
- Keep `Plan`, `runRoundsToRefined`, the `renderPropose`/`renderCritique`/`renderRefine` helpers, and `joinNonEmpty`.
- Delete `Output` struct (was Run's return) IF it's now unreferenced. Otherwise keep.

- [ ] **Step 3: Delete `bp-synthesize.tmpl`**

```bash
git rm internal/engine/prompts/bp-synthesize.tmpl
```

(`Plan` uses `bp-synthesize-one.tmpl`; `Run` was the only user of `bp-synthesize.tmpl`.)

- [ ] **Step 4: Delete tests of `Run`**

In `internal/architect/pipeline_test.go`, delete any `TestRun*` tests. Keep `TestPlanReturnsOneBlueprint` and any tests of the round-1-2-3 helpers that survive.

In `internal/architect/render.go` and its test, delete `RenderForUser` (the multi-blueprint terminal-formatting helper) and its test — it's user-facing and Plan 1 deleted the user-facing path. Keep `Render` (single-blueprint formatter, used by `Plan`).

- [ ] **Step 5: Verify build and tests**

`go build ./...` — clean.
`go test -race ./...` — all packages clean.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "remove(architect): Run + 3-finalist surface — Plan replaces it"
```

---

## Task 6: Update README and architecture doc

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`

- [ ] **Step 1: README — add a brief note about complex-prompt handling**

Find the "Three ways to drive AIOS" section (Plan 1 added it). Add a short subsection or paragraph explaining that complex prompts automatically get an architect pre-pass:

```markdown
### Complex prompts get an extra planning round

When your prompt looks architectural — keywords like "system", "platform",
"refactor large" trigger this, as does length over 200 characters — AIOS
runs an internal divergent-planning round before the 4-stage spec pipeline:
Claude and Codex each propose, cross-critique, refine, then a synthesiser
picks the best of both. The output is one blueprint that feeds the spec
drafts. No 3-choice UI, no extra prompt; just better specs for harder
problems.
```

Voice: terse, factual. No marketing language.

- [ ] **Step 2: docs/architecture.md — add the architect-as-internal subsection**

In `docs/architecture.md`, find the "Specgen" subsection added by Plan 1. Add a sibling subsection:

```markdown
### Architect pre-stage (automatic, complex prompts only)

`specgen.Generate` consults `isComplexPrompt` on entry. If the prompt
matches the heuristic (length > 200 chars or an architectural keyword),
AIOS runs `architect.Plan`: 4 rounds of propose → cross-critique → refine
→ synthesise-to-one. The resulting Blueprint is rendered into the draft
prompt under a "Reference blueprint" section so both stage-1 and stage-2
drafters build from the same plan. Architect failure is non-fatal — a
warning is appended and the pipeline runs without a blueprint.

The package `internal/architect/` retains its name and round structure
from the deleted `aios architect` command; only the user-facing
3-finalist synthesiser was replaced.
```

- [ ] **Step 3: Verify the docs**

```bash
grep -ni "aios architect" README.md docs/architecture.md
# expect: only `internal/architect/` mentions, no `aios architect` user-facing references
```

- [ ] **Step 4: Commit**

```bash
git add README.md docs/architecture.md
git commit -m "docs: complex-prompt architect pre-stage"
```

---

## Task 7: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Build and full test suite**

```bash
go build -o bin/aios ./cmd/aios
go test -race ./...
```

Expected: clean. (Retry `TestRebaseReviewRejects` once if flaky.)

- [ ] **Step 2: Smoke test the heuristic via a unit-style script**

```bash
go test -race ./internal/specgen/ -v -run "TestIsComplex|TestGenerateRuns|TestGenerateSkips"
```

Expected: all pass.

- [ ] **Step 3: Verify the binary --help is unchanged from Plan 1**

```bash
./bin/aios --help
```

Expected: no new flags, no new commands. The architect pre-stage is fully internal.

- [ ] **Step 4: Commit any final tweaks if needed**

If steps 1-3 expose a bug, fix and commit. Otherwise verification-only.

---

## Self-review

**Spec coverage:** every Plan 2 promise mapped to a task.
- Architect logic preserved → Task 2 keeps the rounds, deletes only the user-facing synthesiser.
- Single document out → Task 2 (`Plan` returns one Blueprint), Task 3 (Blueprint feeds drafts).
- Triggered automatically → Task 1 (heuristic), Task 4 (wired into `Generate`).
- No user-facing surface change → Task 7 verifies `--help` unchanged.
- Audit trail preserved → Task 2 keeps `RawArtifacts` map and persists round-4 alongside.
- Plan 3 / Plan 4 capabilities deferred → noted in non-goals.

**Placeholder scan:**
- Task 2 Step 4 has a `panic("implementer: extract from Run lines 53-225")` placeholder. This is intentional — the implementer must do the extraction, and the explicit panic forces them to look at the existing code rather than skipping the step. Acceptable per the plan's TDD discipline.

**Type consistency:**
- `architect.Blueprint`, `architect.Plan`, `architect.Input` defined in Tasks 2; consumed in Task 3 (`specgen.Input.Blueprint`) and Task 4 (`Generate` calls `Plan`).
- `isComplexPrompt`, `complexityCharThreshold`, `complexityKeywords` defined in Task 1; consumed in Task 4.
- `Synthesizer` field added to `specgen.Input` in Task 4.

**Risk tracking:**
- The Task 2 extraction (rounds 1-3 into a helper) is the most error-prone step. The implementer should diff `Run` before and after the extraction to ensure no behaviour change.
- Architect adds latency to every complex prompt. The heuristic is conservative (200 chars / specific keywords) but may still fire on borderline cases. Plan 3 may add `--simple` opt-out if users complain.
- `bp-synthesize.tmpl` deletion in Task 5 is irreversible from this branch's perspective; the new prompt must produce parseable Blueprint output before the old one is removed.

**Out of scope (correctly deferred):**
- Plan 3: repo context, spec quality gate, intake stage, adaptive re-planning.
- Plan 4: comparative evals.
