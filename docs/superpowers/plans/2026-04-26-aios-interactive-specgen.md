# AIOS Interactive Spec Generation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `aios` (no subcommand) launch an interactive REPL that produces a single unified spec/plan via a 4-stage dual-AI pipeline (Claude draft ‖ Codex draft → Codex merge → Claude polish), with `/ship` to hand off to autopilot.

**Architecture:** Two new packages: `internal/specgen/` owns the deterministic 4-stage pipeline (parallel stages 1+2, sequential 3→4, partial-failure fallbacks). New `internal/cli/repl.go` owns the interactive turn loop, slash commands, session persistence, and the `/ship` handoff. Bare `aios` invocation is wired in `internal/cli/root.go` to launch the REPL when no subcommand and no positional args are given.

**Tech Stack:** Go 1.21, cobra (existing CLI framework), text/template + go:embed (matches existing `internal/engine/prompts/` pattern), `internal/engine/FakeEngine` (existing test harness — no new mocking layer).

**Spec:** `docs/superpowers/specs/2026-04-26-aios-interactive-specgen-design.md`

---

## Phase 1 — `internal/specgen` pipeline

### Task 1: Package skeleton and types

**Files:**
- Create: `internal/specgen/types.go`
- Create: `internal/specgen/pipeline.go`

- [ ] **Step 1: Create `internal/specgen/types.go`**

```go
package specgen

import (
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
)

// Input is the full set of inputs to one Generate call.
type Input struct {
	UserRequest    string         // current turn's prompt
	PriorTurns     []Turn         // previous {user message, final spec produced}
	CurrentSpec    string         // empty on first turn; existing .aios/project.md otherwise
	ProjectContext string         // optional repo summary; may be empty
	Claude         engine.Engine  // required
	Codex          engine.Engine  // required
	Recorder       *run.Recorder  // optional; nil = do not persist intermediates
	OnStageStart   func(name string)
	OnStageEnd     func(name string, err error)
}

// Turn is one prior REPL exchange in the same session.
type Turn struct {
	UserMessage string
	FinalSpec   string
}

// Output is what Generate returns.
type Output struct {
	Final       string
	DraftClaude string
	DraftCodex  string
	Merged      string
	Stages      []StageMetric
	Warnings    []string // human-readable notes about partial failures
}

// StageMetric is the audit record for one stage of the pipeline.
type StageMetric struct {
	Name       string // "draft-claude", "draft-codex", "merge", "polish"
	Engine     string // "claude" or "codex"
	DurationMs int
	TokensUsed int
	Err        string // empty = succeeded
	Skipped    bool   // true if this stage did not run because of upstream failure
	Fallback   string // non-empty if this stage took a fallback path
}
```

- [ ] **Step 2: Create `internal/specgen/pipeline.go` with stub Generate**

```go
package specgen

import (
	"context"
	"errors"
)

// Generate runs the 4-stage dual-AI pipeline and returns the unified spec.
// See docs/superpowers/specs/2026-04-26-aios-interactive-specgen-design.md.
func Generate(ctx context.Context, in Input) (Output, error) {
	if in.Claude == nil || in.Codex == nil {
		return Output{}, errors.New("specgen: Claude and Codex engines are required")
	}
	return Output{}, errors.New("specgen: not implemented")
}
```

- [ ] **Step 3: Verify the package builds**

Run: `go build ./internal/specgen/...`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/specgen/types.go internal/specgen/pipeline.go
git commit -m "feat(specgen): package skeleton — Input/Output types and Generate stub"
```

---

### Task 2: Prompt templates

**Files:**
- Create: `internal/specgen/prompts/draft.tmpl`
- Create: `internal/specgen/prompts/merge.tmpl`
- Create: `internal/specgen/prompts/polish.tmpl`
- Create: `internal/specgen/prompts/prompts.go`
- Create: `internal/specgen/prompts/prompts_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/specgen/prompts/prompts_test.go`:

```go
package prompts

import (
	"strings"
	"testing"
)

func TestRenderDraft(t *testing.T) {
	out, err := Render("draft.tmpl", map[string]any{
		"UserRequest":    "build a todo app",
		"CurrentSpec":    "",
		"PriorTurns":     []map[string]string{},
		"ProjectContext": "",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "build a todo app") {
		t.Fatalf("draft template did not interpolate UserRequest; got: %s", out)
	}
}

func TestRenderMerge(t *testing.T) {
	out, err := Render("merge.tmpl", map[string]string{
		"DraftClaude": "DRAFT_A_BODY",
		"DraftCodex":  "DRAFT_B_BODY",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "DRAFT_A_BODY") || !strings.Contains(out, "DRAFT_B_BODY") {
		t.Fatalf("merge template did not include both drafts; got: %s", out)
	}
}

func TestRenderPolish(t *testing.T) {
	out, err := Render("polish.tmpl", map[string]string{"Merged": "MERGED_BODY"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "MERGED_BODY") {
		t.Fatalf("polish template did not interpolate Merged; got: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/prompts/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Create the loader**

Create `internal/specgen/prompts/prompts.go`:

```go
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed *.tmpl
var tmplFS embed.FS

var tmpls = template.Must(template.ParseFS(tmplFS, "*.tmpl"))

// Render executes the named template with data.
func Render(name string, data any) (string, error) {
	var buf bytes.Buffer
	t := tmpls.Lookup(name)
	if t == nil {
		return "", fmt.Errorf("no template named %q", name)
	}
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Create the three templates**

Create `internal/specgen/prompts/draft.tmpl`:

```
You are generating a complete spec/plan for a software requirement.
Be opinionated, structured, and concrete. Use Markdown headings.

Cover at minimum: Goal, Non-goals, User-facing surface, Architecture,
Files to create or modify, Testing strategy, Open questions.

User requirement:
---
{{.UserRequest}}
---
{{if .CurrentSpec}}

The current spec (refining it, not starting fresh):
---
{{.CurrentSpec}}
---
{{end}}{{if .PriorTurns}}

Prior conversation in this session:
{{range .PriorTurns}}- User said: {{.UserMessage}}
{{end}}{{end}}{{if .ProjectContext}}

Repo context:
---
{{.ProjectContext}}
---
{{end}}

Output ONLY the spec. No preamble, no commentary.
```

Create `internal/specgen/prompts/merge.tmpl`:

```
Two independent specs for the same requirement are below. Produce ONE
merged spec that:

- Takes the strongest concrete decisions from each.
- Resolves contradictions in favor of the more specific proposal.
- Adds anything either draft missed.
- Keeps the same Markdown heading structure.

Do not output diffs or commentary — output only the merged spec.

=== Draft A (Claude) ===
{{.DraftClaude}}

=== Draft B (Codex) ===
{{.DraftCodex}}
```

Create `internal/specgen/prompts/polish.tmpl`:

```
Below is a merged spec. Improve clarity, consistency, and completeness
WITHOUT changing scope or removing concrete decisions. Flag any
unresolved ambiguity inline with `> AMBIGUITY: <note>`.

Output ONLY the polished spec.

=== Merged spec ===
{{.Merged}}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/specgen/prompts/...`
Expected: PASS — all three render tests succeed.

- [ ] **Step 6: Commit**

```bash
git add internal/specgen/prompts/
git commit -m "feat(specgen): prompt templates — draft, merge, polish"
```

---

### Task 3: Happy-path Generate (sequential, no parallelism yet)

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Create: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/specgen/pipeline_test.go`:

```go
package specgen

import (
	"context"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestGenerateHappyPath(t *testing.T) {
	claude := &engine.FakeEngine{
		Name_: "claude",
		Script: []engine.InvokeResponse{
			{Text: "DRAFT_A"},   // stage 1
			{Text: "POLISHED"},  // stage 4
		},
	}
	codex := &engine.FakeEngine{
		Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: "DRAFT_B"},   // stage 2
			{Text: "MERGED"},    // stage 3
		},
	}

	out, err := Generate(context.Background(), Input{
		UserRequest: "build it",
		Claude:      claude,
		Codex:       codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED" {
		t.Fatalf("Final = %q, want POLISHED", out.Final)
	}
	if out.DraftClaude != "DRAFT_A" || out.DraftCodex != "DRAFT_B" || out.Merged != "MERGED" {
		t.Fatalf("intermediates wrong: claude=%q codex=%q merged=%q", out.DraftClaude, out.DraftCodex, out.Merged)
	}
	if len(out.Stages) != 4 {
		t.Fatalf("Stages len = %d, want 4", len(out.Stages))
	}
	expectedNames := []string{"draft-claude", "draft-codex", "merge", "polish"}
	for i, s := range out.Stages {
		if s.Name != expectedNames[i] {
			t.Fatalf("Stages[%d].Name = %q, want %q", i, s.Name, expectedNames[i])
		}
		if s.Err != "" {
			t.Fatalf("Stages[%d].Err = %q, want empty", i, s.Err)
		}
	}
	// Stage 4's prompt should reference the merged body verbatim.
	stage4Prompt := claude.Received[1].Prompt
	if !strings.Contains(stage4Prompt, "MERGED") {
		t.Fatalf("polish stage prompt did not include merged body; got: %s", stage4Prompt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/ -run TestGenerateHappyPath`
Expected: FAIL — Generate returns "not implemented".

- [ ] **Step 3: Implement Generate (sequential, no parallelism yet)**

Replace the body of `Generate` in `internal/specgen/pipeline.go`:

```go
package specgen

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/specgen/prompts"
)

func Generate(ctx context.Context, in Input) (Output, error) {
	if in.Claude == nil || in.Codex == nil {
		return Output{}, errors.New("specgen: Claude and Codex engines are required")
	}
	out := Output{}

	priorForTmpl := make([]map[string]string, len(in.PriorTurns))
	for i, t := range in.PriorTurns {
		priorForTmpl[i] = map[string]string{"UserMessage": t.UserMessage}
	}
	draftPrompt, err := prompts.Render("draft.tmpl", map[string]any{
		"UserRequest":    in.UserRequest,
		"CurrentSpec":    in.CurrentSpec,
		"PriorTurns":     priorForTmpl,
		"ProjectContext": in.ProjectContext,
	})
	if err != nil {
		return out, fmt.Errorf("render draft prompt: %w", err)
	}

	// Stage 1: Claude draft
	claudeText, m1 := runStage(ctx, "draft-claude", "claude", in.Claude, draftPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m1)
	if m1.Err != "" {
		return out, fmt.Errorf("stage draft-claude: %s", m1.Err)
	}
	out.DraftClaude = claudeText

	// Stage 2: Codex draft
	codexText, m2 := runStage(ctx, "draft-codex", "codex", in.Codex, draftPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m2)
	if m2.Err != "" {
		return out, fmt.Errorf("stage draft-codex: %s", m2.Err)
	}
	out.DraftCodex = codexText

	// Stage 3: Codex merge
	mergePrompt, err := prompts.Render("merge.tmpl", map[string]string{
		"DraftClaude": out.DraftClaude,
		"DraftCodex":  out.DraftCodex,
	})
	if err != nil {
		return out, fmt.Errorf("render merge prompt: %w", err)
	}
	mergedText, m3 := runStage(ctx, "merge", "codex", in.Codex, mergePrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m3)
	if m3.Err != "" {
		return out, fmt.Errorf("stage merge: %s", m3.Err)
	}
	out.Merged = mergedText

	// Stage 4: Claude polish
	polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": out.Merged})
	if err != nil {
		return out, fmt.Errorf("render polish prompt: %w", err)
	}
	polishedText, m4 := runStage(ctx, "polish", "claude", in.Claude, polishPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m4)
	if m4.Err != "" {
		return out, fmt.Errorf("stage polish: %s", m4.Err)
	}
	out.Final = polishedText

	return out, nil
}

func runStage(ctx context.Context, name, engineName string, eng engine.Engine, prompt string,
	onStart func(string), onEnd func(string, error)) (string, StageMetric) {
	if onStart != nil {
		onStart(name)
	}
	t0 := time.Now()
	resp, err := eng.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: prompt})
	if onEnd != nil {
		onEnd(name, err)
	}
	m := StageMetric{Name: name, Engine: engineName, DurationMs: int(time.Since(t0).Milliseconds())}
	if err != nil {
		m.Err = err.Error()
		return "", m
	}
	m.TokensUsed = resp.UsageTokens
	return resp.Text, m
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/specgen/ -run TestGenerateHappyPath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): sequential 4-stage Generate happy path"
```

---

### Task 4: Parallel stages 1 and 2

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Modify: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Add the failing parallelism test**

Append to `internal/specgen/pipeline_test.go`:

```go
import "sync/atomic"
// (only one import block — merge with existing)

// timingEngine records when each Invoke call started, with a configurable delay.
type timingEngine struct {
	name      string
	delay     time.Duration
	resp      string
	startedAt atomic.Int64 // unix nano when Invoke ran
}

func (e *timingEngine) Name() string { return e.name }
func (e *timingEngine) Invoke(ctx context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	e.startedAt.Store(time.Now().UnixNano())
	time.Sleep(e.delay)
	return &engine.InvokeResponse{Text: e.resp}, nil
}

func TestGenerateRunsDraftsInParallel(t *testing.T) {
	claude := &timingEngine{name: "claude", delay: 100 * time.Millisecond, resp: "DRAFT_A"}
	codex := &timingEngine{name: "codex", delay: 100 * time.Millisecond, resp: "DRAFT_B"}
	// We need extra responses for stages 3 and 4. Wrap with a FakeEngine for those.
	// Simplest: use the timing engines for stages 1/2 only, and a separate
	// pair for stages 3/4 — but Generate uses the same Engine for stage 4 as stage 1.
	// Solution: use a multiResponseTimingEngine that returns the next scripted response.

	t.Skip("Replaced by TestGenerateDraftsConcurrent below — keeping skeleton for clarity")
}

// multiTimingEngine returns scripted responses in order; the first call records
// its start time; all calls share the same delay.
type multiTimingEngine struct {
	name      string
	delay     time.Duration
	responses []string
	startedAt []int64
	mu        sync.Mutex
	idx       int
}

func (e *multiTimingEngine) Name() string { return e.name }
func (e *multiTimingEngine) Invoke(ctx context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	e.mu.Lock()
	if e.idx >= len(e.responses) {
		e.mu.Unlock()
		return nil, errors.New("multiTimingEngine exhausted")
	}
	now := time.Now().UnixNano()
	e.startedAt = append(e.startedAt, now)
	r := e.responses[e.idx]
	e.idx++
	e.mu.Unlock()
	time.Sleep(e.delay)
	return &engine.InvokeResponse{Text: r}, nil
}

func TestGenerateDraftsConcurrent(t *testing.T) {
	claude := &multiTimingEngine{name: "claude", delay: 80 * time.Millisecond, responses: []string{"DRAFT_A", "POLISHED"}}
	codex := &multiTimingEngine{name: "codex", delay: 80 * time.Millisecond, responses: []string{"DRAFT_B", "MERGED"}}

	start := time.Now()
	out, err := Generate(context.Background(), Input{UserRequest: "x", Claude: claude, Codex: codex})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED" {
		t.Fatalf("Final = %q", out.Final)
	}
	// Sequential lower bound: 4 * 80ms = 320ms. Parallel stages 1&2 overlap
	// so the total should be ~3 * 80ms = 240ms. Allow generous slack for CI:
	// fail only if total exceeds 350ms (which would prove sequential).
	if elapsed > 350*time.Millisecond {
		t.Fatalf("Generate took %v — stages 1 and 2 ran sequentially (expected parallel)", elapsed)
	}
	// Verify both first-call start times are within 30ms of each other.
	if len(claude.startedAt) < 1 || len(codex.startedAt) < 1 {
		t.Fatalf("missing start times")
	}
	skew := claude.startedAt[0] - codex.startedAt[0]
	if skew < 0 {
		skew = -skew
	}
	if skew > int64(30*time.Millisecond) {
		t.Fatalf("draft start skew = %v, want < 30ms", time.Duration(skew))
	}
}
```

Add the missing imports at the top of the file:

```go
import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)
```

(Drop `sync/atomic` if unused after removing the skipped test; the simpler `multiTimingEngine` uses a mutex instead.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/ -run TestGenerateDraftsConcurrent -v`
Expected: FAIL — current implementation is sequential, takes ~320ms.

- [ ] **Step 3: Make stages 1 and 2 parallel**

Replace the stage 1 and stage 2 sections of `Generate` in `internal/specgen/pipeline.go` with:

```go
	// Stages 1 and 2 in parallel.
	type draftResult struct {
		text   string
		metric StageMetric
	}
	claudeCh := make(chan draftResult, 1)
	codexCh := make(chan draftResult, 1)
	go func() {
		text, m := runStage(ctx, "draft-claude", "claude", in.Claude, draftPrompt, in.OnStageStart, in.OnStageEnd)
		claudeCh <- draftResult{text, m}
	}()
	go func() {
		text, m := runStage(ctx, "draft-codex", "codex", in.Codex, draftPrompt, in.OnStageStart, in.OnStageEnd)
		codexCh <- draftResult{text, m}
	}()
	c := <-claudeCh
	x := <-codexCh
	out.Stages = append(out.Stages, c.metric, x.metric)
	if c.metric.Err != "" {
		return out, fmt.Errorf("stage draft-claude: %s", c.metric.Err)
	}
	if x.metric.Err != "" {
		return out, fmt.Errorf("stage draft-codex: %s", x.metric.Err)
	}
	out.DraftClaude = c.text
	out.DraftCodex = x.text
```

(Remove the original sequential stage-1 and stage-2 blocks.)

- [ ] **Step 4: Run both tests to verify they pass**

Run: `go test ./internal/specgen/ -v`
Expected: PASS for both `TestGenerateHappyPath` and `TestGenerateDraftsConcurrent`.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): run draft stages 1 and 2 in parallel"
```

---

### Task 5: Disk persistence of intermediates

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Modify: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Add the failing persistence test**

Append to `internal/specgen/pipeline_test.go`:

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
	// ...existing imports
	"github.com/MoonCodeMaster/AIOS/internal/run"
)

func TestGeneratePersistsIntermediates(t *testing.T) {
	dir := t.TempDir()
	rec, err := run.Open(dir, "test-run")
	if err != nil {
		t.Fatalf("run.Open: %v", err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}

	_, err = Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex, Recorder: rec,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := map[string]string{
		"specgen/draft-claude.md": "DRAFT_A",
		"specgen/draft-codex.md":  "DRAFT_B",
		"specgen/merged.md":       "MERGED",
		"specgen/final.md":        "POLISHED",
	}
	for rel, body := range want {
		p := filepath.Join(dir, "test-run", rel)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != body {
			t.Fatalf("%s = %q, want %q", rel, got, body)
		}
	}

	stagesPath := filepath.Join(dir, "test-run", "specgen", "stages.json")
	raw, err := os.ReadFile(stagesPath)
	if err != nil {
		t.Fatalf("read stages.json: %v", err)
	}
	var stages []StageMetric
	if err := json.Unmarshal(raw, &stages); err != nil {
		t.Fatalf("unmarshal stages.json: %v", err)
	}
	if len(stages) != 4 {
		t.Fatalf("stages.json had %d entries, want 4", len(stages))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/ -run TestGeneratePersistsIntermediates`
Expected: FAIL — files not written.

- [ ] **Step 3: Implement persistence**

Add to the end of `Generate` in `internal/specgen/pipeline.go`, just before `return out, nil`:

```go
	if in.Recorder != nil {
		_ = in.Recorder.WriteFile("specgen/draft-claude.md", []byte(out.DraftClaude))
		_ = in.Recorder.WriteFile("specgen/draft-codex.md", []byte(out.DraftCodex))
		_ = in.Recorder.WriteFile("specgen/merged.md", []byte(out.Merged))
		_ = in.Recorder.WriteFile("specgen/final.md", []byte(out.Final))
		if data, err := json.MarshalIndent(out.Stages, "", "  "); err == nil {
			_ = in.Recorder.WriteFile("specgen/stages.json", data)
		}
	}
```

Add `"encoding/json"` to the imports at the top of `pipeline.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/specgen/ -v`
Expected: all pipeline tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): persist intermediate drafts and stage metrics to run dir"
```

---

### Task 6: Single-draft fallbacks (stage 1 or 2 fails)

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Modify: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/specgen/pipeline_test.go`:

```go
// errEngine returns the same error on every call.
type errEngine struct {
	name string
	err  error
}

func (e *errEngine) Name() string { return e.name }
func (e *errEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	return nil, e.err
}

func TestGenerateClaudeDraftFailsThenSingleDraftFlow(t *testing.T) {
	claude := &errEngine{name: "claude", err: errors.New("claude offline")}
	// Codex is used for: stage-2 draft, stage-3 merge SKIPPED (only one draft),
	// stage-4 polish — wait, polish is Claude's job. With Claude broken we
	// cannot polish. Per design: skip merge, run polish on the surviving draft.
	// Polish is done by Claude — but Claude is broken.
	// Per spec error handling: when one drafter fails, the OTHER engine takes
	// over polish too (i.e. the polish stage is run by whichever engine still
	// works). Wire this in the impl.
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"},
		{Text: "POLISHED_BY_CODEX"}, // codex stands in for polish too
	}}

	out, err := Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED_BY_CODEX" {
		t.Fatalf("Final = %q, want POLISHED_BY_CODEX", out.Final)
	}
	if out.DraftCodex != "DRAFT_B" {
		t.Fatalf("DraftCodex = %q", out.DraftCodex)
	}
	if out.DraftClaude != "" {
		t.Fatalf("DraftClaude should be empty when Claude failed; got %q", out.DraftClaude)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "Claude") {
		t.Fatalf("expected warning about Claude failure; got %v", out.Warnings)
	}
	// Stage 1 should be marked failed; stage 3 (merge) should be marked Skipped.
	stagesByName := map[string]StageMetric{}
	for _, s := range out.Stages {
		stagesByName[s.Name] = s
	}
	if s := stagesByName["draft-claude"]; s.Err == "" {
		t.Fatalf("draft-claude stage Err should be non-empty")
	}
	if s := stagesByName["merge"]; !s.Skipped {
		t.Fatalf("merge stage should be Skipped when only one draft survives")
	}
}

func TestGenerateCodexDraftFailsThenSingleDraftFlow(t *testing.T) {
	codex := &errEngine{name: "codex", err: errors.New("codex offline")}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"},
		{Text: "POLISHED_BY_CLAUDE"},
	}}

	out, err := Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED_BY_CLAUDE" {
		t.Fatalf("Final = %q", out.Final)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "Codex") {
		t.Fatalf("expected warning about Codex failure; got %v", out.Warnings)
	}
}
```

**Decision baked in by these tests:** when one drafter fails, the surviving engine also runs the polish step (since the merge is skipped, "polish" is the only remaining stage and we use whichever engine is still working). This is a refinement of the spec's error-handling table and is documented in Task 19 (docs).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/specgen/ -run "TestGenerate(Claude|Codex)DraftFails"`
Expected: FAIL — current code returns the error instead of falling back.

- [ ] **Step 3: Implement the fallback**

Replace the section of `Generate` in `internal/specgen/pipeline.go` from "Stages 1 and 2 in parallel" through the polish stage with:

```go
	// Stages 1 and 2 in parallel.
	type draftResult struct {
		text   string
		metric StageMetric
	}
	claudeCh := make(chan draftResult, 1)
	codexCh := make(chan draftResult, 1)
	go func() {
		text, m := runStage(ctx, "draft-claude", "claude", in.Claude, draftPrompt, in.OnStageStart, in.OnStageEnd)
		claudeCh <- draftResult{text, m}
	}()
	go func() {
		text, m := runStage(ctx, "draft-codex", "codex", in.Codex, draftPrompt, in.OnStageStart, in.OnStageEnd)
		codexCh <- draftResult{text, m}
	}()
	c := <-claudeCh
	x := <-codexCh
	out.Stages = append(out.Stages, c.metric, x.metric)
	out.DraftClaude = c.text
	out.DraftCodex = x.text

	claudeOK := c.metric.Err == ""
	codexOK := x.metric.Err == ""

	switch {
	case !claudeOK && !codexOK:
		out.Stages = append(out.Stages,
			StageMetric{Name: "merge", Engine: "codex", Skipped: true},
			StageMetric{Name: "polish", Engine: "claude", Skipped: true},
		)
		persist(in.Recorder, out)
		return out, fmt.Errorf("both drafters failed: claude=%q codex=%q", c.metric.Err, x.metric.Err)

	case !claudeOK || !codexOK:
		var surviving, survName string
		var survEngine engine.Engine
		var failedName string
		if claudeOK {
			surviving, survName, survEngine = c.text, "claude", in.Claude
			failedName = "Codex"
		} else {
			surviving, survName, survEngine = x.text, "codex", in.Codex
			failedName = "Claude"
		}
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("%s draft failed; spec built from %s alone — consider rerunning.", failedName, survName))
		out.Stages = append(out.Stages, StageMetric{
			Name: "merge", Engine: "codex", Skipped: true, Fallback: "single-draft",
		})
		// Polish the surviving draft with whichever engine still works.
		polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": surviving})
		if err != nil {
			return out, fmt.Errorf("render polish prompt: %w", err)
		}
		polishedText, m4 := runStage(ctx, "polish", survName, survEngine, polishPrompt, in.OnStageStart, in.OnStageEnd)
		out.Stages = append(out.Stages, m4)
		if m4.Err != "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("Polish step failed; spec is the surviving draft. (%s)", m4.Err))
			out.Final = surviving
		} else {
			out.Final = polishedText
		}
		persist(in.Recorder, out)
		return out, nil
	}

	// Both drafts succeeded — normal merge + polish path.
	mergePrompt, err := prompts.Render("merge.tmpl", map[string]string{
		"DraftClaude": out.DraftClaude,
		"DraftCodex":  out.DraftCodex,
	})
	if err != nil {
		return out, fmt.Errorf("render merge prompt: %w", err)
	}
	mergedText, m3 := runStage(ctx, "merge", "codex", in.Codex, mergePrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m3)
	if m3.Err != "" {
		// Stage 3 fallback handled in Task 7.
		return out, fmt.Errorf("stage merge: %s", m3.Err)
	}
	out.Merged = mergedText

	polishPrompt, err := prompts.Render("polish.tmpl", map[string]string{"Merged": out.Merged})
	if err != nil {
		return out, fmt.Errorf("render polish prompt: %w", err)
	}
	polishedText, m4 := runStage(ctx, "polish", "claude", in.Claude, polishPrompt, in.OnStageStart, in.OnStageEnd)
	out.Stages = append(out.Stages, m4)
	if m4.Err != "" {
		// Stage 4 fallback handled in Task 8.
		return out, fmt.Errorf("stage polish: %s", m4.Err)
	}
	out.Final = polishedText
	persist(in.Recorder, out)
	return out, nil
}

// persist writes intermediate drafts and stage metrics to the recorder.
// Extracted so the various return paths can call it once each.
func persist(rec *run.Recorder, out Output) {
	if rec == nil {
		return
	}
	if out.DraftClaude != "" {
		_ = rec.WriteFile("specgen/draft-claude.md", []byte(out.DraftClaude))
	}
	if out.DraftCodex != "" {
		_ = rec.WriteFile("specgen/draft-codex.md", []byte(out.DraftCodex))
	}
	if out.Merged != "" {
		_ = rec.WriteFile("specgen/merged.md", []byte(out.Merged))
	}
	if out.Final != "" {
		_ = rec.WriteFile("specgen/final.md", []byte(out.Final))
	}
	if data, err := json.MarshalIndent(out.Stages, "", "  "); err == nil {
		_ = rec.WriteFile("specgen/stages.json", data)
	}
}
```

Add the `"github.com/MoonCodeMaster/AIOS/internal/run"` import. Remove the inline persistence block from Task 5 — it is now handled by `persist`.

- [ ] **Step 4: Run all pipeline tests**

Run: `go test ./internal/specgen/ -v`
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): single-draft fallback when one drafter fails"
```

---

### Task 7: Merge stage fallback (longer draft wins)

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Modify: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/specgen/pipeline_test.go`:

```go
// scriptedEngine returns scripted responses that may be errors.
type scriptedEngine struct {
	name      string
	responses []scriptedResponse
	idx       int
	mu        sync.Mutex
}

type scriptedResponse struct {
	text string
	err  error
}

func (e *scriptedEngine) Name() string { return e.name }
func (e *scriptedEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.idx >= len(e.responses) {
		return nil, errors.New("scripted exhausted")
	}
	r := e.responses[e.idx]
	e.idx++
	if r.err != nil {
		return nil, r.err
	}
	return &engine.InvokeResponse{Text: r.text}, nil
}

func TestGenerateMergeFailsFallsBackToLongerDraft(t *testing.T) {
	claude := &scriptedEngine{name: "claude", responses: []scriptedResponse{
		{text: "DRAFT_A_short"},
		{text: "POLISHED"}, // stage 4 polishes whichever fallback we picked
	}}
	codex := &scriptedEngine{name: "codex", responses: []scriptedResponse{
		{text: "DRAFT_B_this_one_is_clearly_longer_than_A"}, // stage 2
		{err: errors.New("codex merge failed")},              // stage 3
	}}

	out, err := Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "POLISHED" {
		t.Fatalf("Final = %q", out.Final)
	}
	// Merged should be the longer draft (Codex's).
	if out.Merged != "DRAFT_B_this_one_is_clearly_longer_than_A" {
		t.Fatalf("Merged = %q, want longer draft as fallback", out.Merged)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "Merge") {
		t.Fatalf("expected merge-fallback warning; got %v", out.Warnings)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/ -run TestGenerateMergeFails`
Expected: FAIL — Generate currently returns an error on merge failure.

- [ ] **Step 3: Implement the merge fallback**

In `internal/specgen/pipeline.go`, replace the merge-stage error block:

```go
	if m3.Err != "" {
		// Stage 3 fallback handled in Task 7.
		return out, fmt.Errorf("stage merge: %s", m3.Err)
	}
	out.Merged = mergedText
```

with:

```go
	if m3.Err != "" {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("Merge step failed; using longer draft as fallback. (%s)", m3.Err))
		if len(out.DraftCodex) >= len(out.DraftClaude) {
			out.Merged = out.DraftCodex
		} else {
			out.Merged = out.DraftClaude
		}
		// Mark stage as fallback for the audit trail.
		out.Stages[len(out.Stages)-1].Fallback = "longer-draft"
	} else {
		out.Merged = mergedText
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/specgen/ -v`
Expected: all pipeline tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): merge fallback uses longer draft when stage 3 fails"
```

---

### Task 8: Polish stage fallback (return merged version)

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Modify: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/specgen/pipeline_test.go`:

```go
func TestGeneratePolishFailsReturnsMerged(t *testing.T) {
	claude := &scriptedEngine{name: "claude", responses: []scriptedResponse{
		{text: "DRAFT_A"},
		{err: errors.New("claude polish failed")}, // stage 4
	}}
	codex := &scriptedEngine{name: "codex", responses: []scriptedResponse{
		{text: "DRAFT_B"},
		{text: "MERGED_FINAL"},
	}}

	out, err := Generate(context.Background(), Input{
		UserRequest: "x", Claude: claude, Codex: codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Final != "MERGED_FINAL" {
		t.Fatalf("Final = %q, want MERGED_FINAL (polish fallback)", out.Final)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "Polish") {
		t.Fatalf("expected polish-fallback warning; got %v", out.Warnings)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/ -run TestGeneratePolishFails`
Expected: FAIL.

- [ ] **Step 3: Implement the polish fallback**

In `internal/specgen/pipeline.go`, replace the polish error block (in the "both drafts succeeded" path):

```go
	if m4.Err != "" {
		// Stage 4 fallback handled in Task 8.
		return out, fmt.Errorf("stage polish: %s", m4.Err)
	}
	out.Final = polishedText
	persist(in.Recorder, out)
	return out, nil
```

with:

```go
	if m4.Err != "" {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("Polish step failed; spec is the merged version. (%s)", m4.Err))
		out.Final = out.Merged
	} else {
		out.Final = polishedText
	}
	persist(in.Recorder, out)
	return out, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/specgen/ -v`
Expected: all pipeline tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): polish fallback returns merged version when stage 4 fails"
```

---

## Phase 2 — REPL command

### Task 9: Session struct and persistence

**Files:**
- Create: `internal/cli/repl_session.go`
- Create: `internal/cli/repl_session_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/repl_session_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		ID:         "session-x",
		Created:    time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		SessionDir: filepath.Join(dir, "session-x"),
		SpecPath:   filepath.Join(dir, "project.md"),
		Turns: []SessionTurn{
			{Timestamp: time.Date(2026, 4, 26, 12, 1, 0, 0, time.UTC), UserMessage: "hello", SpecAfter: "SPEC1", RunID: "run-1"},
		},
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadSession(s.SessionDir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got.ID != s.ID || len(got.Turns) != 1 || got.Turns[0].UserMessage != "hello" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLatestSessionPicksMostRecent(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"2026-04-26T10-00-00", "2026-04-26T11-00-00", "2026-04-26T09-00-00"} {
		s := &Session{ID: id, SessionDir: filepath.Join(dir, id)}
		if err := os.MkdirAll(s.SessionDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LatestSession(dir)
	if err != nil {
		t.Fatalf("LatestSession: %v", err)
	}
	if got.ID != "2026-04-26T11-00-00" {
		t.Fatalf("LatestSession = %q, want 2026-04-26T11-00-00", got.ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestSession`
Expected: FAIL — types do not exist.

- [ ] **Step 3: Create the session module**

Create `internal/cli/repl_session.go`:

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Session is the state of one REPL session, persisted between turns and
// restorable across crashes via `aios --resume`.
type Session struct {
	ID         string        `json:"id"`
	Created    time.Time     `json:"created"`
	SessionDir string        `json:"session_dir"`
	SpecPath   string        `json:"spec_path"`
	Turns      []SessionTurn `json:"turns"`
}

type SessionTurn struct {
	Timestamp   time.Time `json:"timestamp"`
	UserMessage string    `json:"user_message"`
	SpecAfter   string    `json:"spec_after"`
	RunID       string    `json:"run_id"`
}

// Save writes the session to <SessionDir>/session.json.
func (s *Session) Save() error {
	if err := os.MkdirAll(s.SessionDir, 0o755); err != nil {
		return fmt.Errorf("mkdir session dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.SessionDir, "session.json"), data, 0o644)
}

// LoadSession reads a session.json from a session directory.
func LoadSession(dir string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("read session.json: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session.json: %w", err)
	}
	return &s, nil
}

// LatestSession returns the most recent session in sessionsDir, identified
// by the lexicographic ordering of session IDs (timestamp-prefixed).
func LatestSession(sessionsDir string) (*Session, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no sessions in %s", sessionsDir)
	}
	sort.Strings(ids)
	return LoadSession(filepath.Join(sessionsDir, ids[len(ids)-1]))
}

// NewSessionID returns a new timestamp-based session ID.
func NewSessionID() string {
	return time.Now().UTC().Format("2006-01-02T15-04-05")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestSession -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl_session.go internal/cli/repl_session_test.go
git commit -m "feat(repl): session struct with save/load/latest helpers"
```

---

### Task 10: Slash-command parser

**Files:**
- Create: `internal/cli/repl_slash.go`
- Create: `internal/cli/repl_slash_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/repl_slash_test.go`:

```go
package cli

import "testing"

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		in   string
		want SlashCommand
	}{
		{"/ship", SlashShip},
		{"/show", SlashShow},
		{"/clear", SlashClear},
		{"/help", SlashHelp},
		{"/exit", SlashExit},
		{"/quit", SlashExit},
		{"/SHIP", SlashShip},        // case-insensitive
		{"  /ship  ", SlashShip},    // trimmed
		{"hello", SlashNone},        // not a slash command
		{"/unknown", SlashUnknown},
		{"", SlashNone},
	}
	for _, tt := range tests {
		got := ParseSlash(tt.in)
		if got != tt.want {
			t.Errorf("ParseSlash(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestParseSlashCommand`
Expected: FAIL — types do not exist.

- [ ] **Step 3: Create the parser**

Create `internal/cli/repl_slash.go`:

```go
package cli

import "strings"

type SlashCommand int

const (
	SlashNone SlashCommand = iota
	SlashUnknown
	SlashShip
	SlashShow
	SlashClear
	SlashHelp
	SlashExit
)

// ParseSlash returns SlashNone if the input is not a slash command,
// SlashUnknown if it starts with "/" but is not recognised, and the
// matching SlashCommand otherwise.
func ParseSlash(s string) SlashCommand {
	s = strings.TrimSpace(s)
	if s == "" || !strings.HasPrefix(s, "/") {
		return SlashNone
	}
	switch strings.ToLower(s) {
	case "/ship":
		return SlashShip
	case "/show":
		return SlashShow
	case "/clear":
		return SlashClear
	case "/help":
		return SlashHelp
	case "/exit", "/quit":
		return SlashExit
	default:
		return SlashUnknown
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestParseSlashCommand -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl_slash.go internal/cli/repl_slash_test.go
git commit -m "feat(repl): slash command parser"
```

---

### Task 11: Turn loop core

**Files:**
- Create: `internal/cli/repl.go`
- Create: `internal/cli/repl_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/repl_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

// runReplWith runs the REPL with mocked stdin/stdout and the given engines.
// It returns the stdout buffer for assertions.
func runReplWith(t *testing.T, wd, stdin string, claude, codex engine.Engine) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	in := strings.NewReader(stdin)
	r := &Repl{
		Wd:       wd,
		In:       in,
		Out:      stdout,
		Claude:   claude,
		Codex:    codex,
		NoColor:  true,
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return stdout.String()
}

func TestReplOneTurnWritesSpec(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	// Two lines: the request, then /exit.
	stdin := "build a thing\n\n/exit\n"
	out := runReplWith(t, wd, stdin, claude, codex)

	specBody, err := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if err != nil {
		t.Fatalf("read project.md: %v", err)
	}
	if string(specBody) != "POLISHED" {
		t.Fatalf("project.md = %q, want POLISHED", specBody)
	}
	if !strings.Contains(out, "Spec updated") {
		t.Fatalf("expected 'Spec updated' summary in stdout; got: %s", out)
	}
}
```

**Note on the input format:** the REPL accepts a multi-line message terminated by a blank line (matches Claude CLI). So `"build a thing\n\n/exit\n"` is one user message ("build a thing") followed by a slash command.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestReplOneTurnWritesSpec`
Expected: FAIL — Repl type does not exist.

- [ ] **Step 3: Create the REPL**

Create `internal/cli/repl.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
)

// Repl is one interactive AIOS session.
type Repl struct {
	Wd      string
	In      io.Reader
	Out     io.Writer
	Claude  engine.Engine
	Codex   engine.Engine
	NoColor bool

	session *Session
}

// Run executes the REPL turn loop until /exit, EOF, or /ship.
func (r *Repl) Run(ctx context.Context) error {
	if err := r.bootSession(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(r.In)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // allow long pasted prompts

	fmt.Fprintln(r.Out, "aios — type a requirement, blank line to submit. /help for commands.")
	for {
		msg, ok := readMessage(scanner, r.Out)
		if !ok {
			return nil
		}
		switch ParseSlash(msg) {
		case SlashExit:
			fmt.Fprintln(r.Out, "bye.")
			return nil
		case SlashHelp:
			r.printHelp()
			continue
		case SlashShow:
			r.printSpec()
			continue
		case SlashClear:
			r.session.Turns = nil
			_ = r.session.Save()
			fmt.Fprintln(r.Out, "session cleared.")
			continue
		case SlashShip:
			return r.ship(ctx)
		case SlashUnknown:
			fmt.Fprintf(r.Out, "unknown slash command. /help for the list.\n")
			continue
		}
		// Natural-language input → run the pipeline.
		if err := r.runTurn(ctx, msg); err != nil {
			fmt.Fprintf(r.Out, "turn failed: %v\n", err)
		}
	}
}

func (r *Repl) bootSession() error {
	if r.session != nil {
		return nil
	}
	id := NewSessionID()
	r.session = &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: filepath.Join(r.Wd, ".aios", "sessions", id),
		SpecPath:   filepath.Join(r.Wd, ".aios", "project.md"),
	}
	return r.session.Save()
}

func (r *Repl) runTurn(ctx context.Context, msg string) error {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(r.Wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}
	currentSpec := ""
	if data, err := os.ReadFile(r.session.SpecPath); err == nil {
		currentSpec = string(data)
	}
	prior := make([]specgen.Turn, len(r.session.Turns))
	for i, t := range r.session.Turns {
		prior[i] = specgen.Turn{UserMessage: t.UserMessage, FinalSpec: t.SpecAfter}
	}
	in := specgen.Input{
		UserRequest:  msg,
		PriorTurns:   prior,
		CurrentSpec:  currentSpec,
		Claude:       r.Claude,
		Codex:        r.Codex,
		Recorder:     rec,
		OnStageStart: func(name string) { fmt.Fprintf(r.Out, "  · %s …\n", name) },
		OnStageEnd:   func(_ string, _ error) {},
	}
	out, err := specgen.Generate(ctx, in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.session.SpecPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(r.session.SpecPath, []byte(out.Final), 0o644); err != nil {
		return err
	}
	r.session.Turns = append(r.session.Turns, SessionTurn{
		Timestamp: time.Now().UTC(), UserMessage: msg, SpecAfter: out.Final, RunID: runID,
	})
	if err := r.session.Save(); err != nil {
		return err
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(r.Out, "  ! %s\n", w)
	}
	lineCount := strings.Count(out.Final, "\n") + 1
	fmt.Fprintf(r.Out, "Spec updated (%d lines). /show to view, /ship to implement, or refine with another message.\n", lineCount)
	return nil
}

func (r *Repl) printSpec() {
	data, err := os.ReadFile(r.session.SpecPath)
	if err != nil {
		fmt.Fprintf(r.Out, "no spec yet.\n")
		return
	}
	fmt.Fprintln(r.Out, "---")
	fmt.Fprintln(r.Out, string(data))
	fmt.Fprintln(r.Out, "---")
}

func (r *Repl) printHelp() {
	fmt.Fprintln(r.Out, "commands:")
	fmt.Fprintln(r.Out, "  /show   print current spec")
	fmt.Fprintln(r.Out, "  /clear  discard session, start fresh")
	fmt.Fprintln(r.Out, "  /ship   hand the spec to autopilot (decompose → run → PR)")
	fmt.Fprintln(r.Out, "  /exit   leave the REPL")
	fmt.Fprintln(r.Out, "  /help   this list")
}

func (r *Repl) ship(_ context.Context) error {
	// Implemented in Task 14.
	fmt.Fprintln(r.Out, "/ship not yet wired (see Task 14)")
	return nil
}

// readMessage reads lines until a blank line (submit) or EOF. Returns
// (message, true) on submit; ("", false) on EOF.
func readMessage(s *bufio.Scanner, out io.Writer) (string, bool) {
	fmt.Fprint(out, "> ")
	var lines []string
	for s.Scan() {
		line := s.Text()
		if line == "" {
			return strings.Join(lines, "\n"), true
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestReplOneTurnWritesSpec -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl.go internal/cli/repl_test.go
git commit -m "feat(repl): turn loop with one-turn happy path and slash dispatch"
```

---

### Task 12: Slash commands `/show`, `/clear`, `/help`

**Files:**
- Modify: `internal/cli/repl_test.go`

The implementation already exists from Task 11. This task adds explicit tests for each slash command's behavior.

- [ ] **Step 1: Add slash-command tests**

Append to `internal/cli/repl_test.go`:

```go
func TestReplShowPrintsSpec(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, ".aios", "project.md"), []byte("EXISTING_SPEC_BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runReplWith(t, wd, "/show\n\n/exit\n",
		&engine.FakeEngine{Name_: "claude"}, &engine.FakeEngine{Name_: "codex"})
	if !strings.Contains(out, "EXISTING_SPEC_BODY") {
		t.Fatalf("/show did not print spec body; got: %s", out)
	}
}

func TestReplClearDropsTurns(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	stdin := "first message\n\n/clear\n\n/exit\n"
	out := runReplWith(t, wd, stdin, claude, codex)
	if !strings.Contains(out, "session cleared.") {
		t.Fatalf("/clear did not print confirmation; got: %s", out)
	}
}

func TestReplHelpListsCommands(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := runReplWith(t, wd, "/help\n\n/exit\n",
		&engine.FakeEngine{Name_: "claude"}, &engine.FakeEngine{Name_: "codex"})
	for _, expected := range []string{"/show", "/clear", "/ship", "/exit"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("/help missing %s; got: %s", expected, out)
		}
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -run "TestReplShow|TestReplClear|TestReplHelp" -v`
Expected: PASS for all three.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/repl_test.go
git commit -m "test(repl): cover /show, /clear, /help slash commands"
```

---

### Task 13: CLI-missing refusal

**Files:**
- Modify: `internal/cli/repl.go`
- Modify: `internal/cli/repl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/repl_test.go`:

```go
import "os/exec"

func TestReplRefusesWhenCLIMissing(t *testing.T) {
	wd := t.TempDir()
	r := &Repl{
		Wd:           wd,
		In:           strings.NewReader(""),
		Out:          &bytes.Buffer{},
		ClaudeBinary: "this-binary-does-not-exist-aios-test",
		CodexBinary:  "codex", // assumed present or whatever; first check should fail
		LookPath:     exec.LookPath,
	}
	err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("Run should have returned an error when claude binary is missing")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("error should mention missing claude; got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (compile failure expected)**

Run: `go test ./internal/cli/ -run TestReplRefuses`
Expected: FAIL — `ClaudeBinary`, `CodexBinary`, `LookPath` fields do not exist.

- [ ] **Step 3: Add binary-existence check**

In `internal/cli/repl.go`:

1. Add to the `Repl` struct:

```go
	ClaudeBinary string
	CodexBinary  string
	LookPath     func(string) (string, error) // injectable for tests; defaults to exec.LookPath
```

2. Add at the top of `Run`, before `bootSession`:

```go
	if r.LookPath == nil {
		r.LookPath = exec.LookPath
	}
	if r.ClaudeBinary != "" {
		if _, err := r.LookPath(r.ClaudeBinary); err != nil {
			return fmt.Errorf("claude CLI not found (%s): run `aios doctor`", r.ClaudeBinary)
		}
	}
	if r.CodexBinary != "" {
		if _, err := r.LookPath(r.CodexBinary); err != nil {
			return fmt.Errorf("codex CLI not found (%s): run `aios doctor`", r.CodexBinary)
		}
	}
```

3. Add `"os/exec"` to the imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestReplRefuses -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl.go internal/cli/repl_test.go
git commit -m "feat(repl): refuse to launch when claude or codex binary is missing"
```

---

### Task 14: `/ship` handoff to autopilot

**Files:**
- Modify: `internal/cli/repl.go`
- Modify: `internal/cli/repl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/repl_test.go`:

```go
func TestReplShipCallsAutopilotHook(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, ".aios", "project.md"), []byte("SPEC"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader("/ship\n\n"),
		Out:    &bytes.Buffer{},
		Claude: &engine.FakeEngine{Name_: "claude"},
		Codex:  &engine.FakeEngine{Name_: "codex"},
		ShipFn: func(_ context.Context, wd string) error {
			if wd == "" {
				t.Fatalf("ShipFn called with empty wd")
			}
			called = true
			return nil
		},
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatalf("ShipFn was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestReplShipCalls`
Expected: FAIL — `ShipFn` field does not exist.

- [ ] **Step 3: Wire `/ship`**

In `internal/cli/repl.go`:

1. Add to the `Repl` struct:

```go
	ShipFn func(ctx context.Context, wd string) error // injectable for tests; defaults to runAutopilotShip
```

2. Replace the placeholder `ship` method:

```go
func (r *Repl) ship(ctx context.Context) error {
	if r.ShipFn == nil {
		r.ShipFn = runAutopilotShip
	}
	fmt.Fprintln(r.Out, "shipping spec to autopilot…")
	return r.ShipFn(ctx, r.Wd)
}

// runAutopilotShip drives `aios run --autopilot --merge` against the spec
// already on disk at <wd>/.aios/project.md. Equivalent to typing `aios
// autopilot` after `aios new --auto` has run.
func runAutopilotShip(_ context.Context, wd string) error {
	// Decompose first (architect/new normally does this; here the spec is
	// already on disk so we only need decompose + run).
	if err := decomposeOnly(wd); err != nil {
		return fmt.Errorf("decompose: %w", err)
	}
	runCmd := newRunCmd()
	if err := runCmd.Flags().Set("autopilot", "true"); err != nil {
		return fmt.Errorf("set --autopilot: %w", err)
	}
	if err := runCmd.Flags().Set("merge", "true"); err != nil {
		return fmt.Errorf("set --merge: %w", err)
	}
	return runMain(runCmd, nil)
}

// decomposeOnly turns the existing .aios/project.md into task files,
// reusing the same decompose prompt as `aios new` but skipping
// brainstorm and spec-synth (the spec is already final).
func decomposeOnly(wd string) error {
	specPath := filepath.Join(wd, ".aios", "project.md")
	specBody, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read project.md: %w", err)
	}
	cfg, err := loadConfigForWd(wd)
	if err != nil {
		return err
	}
	codex := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	dPrompt, err := promptsRender("decompose.tmpl", map[string]string{"Spec": string(specBody)})
	if err != nil {
		return err
	}
	dRes, err := codex.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return err
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	if _, err := writeTaskFiles(tasksDir, dRes.Text); err != nil {
		return err
	}
	return commitNewSpec(wd, cfg.Project.StagingBranch, "interactive session")
}
```

**On the `loadConfigForWd` and `promptsRender` references:** these are thin internal helpers. Add them at the bottom of `repl.go`:

```go
func loadConfigForWd(wd string) (*config.Config, error) {
	return config.Load(filepath.Join(wd, ".aios", "config.toml"))
}

// promptsRender is a local alias to keep the import list shorter where
// the engine prompts package is the only thing being used.
func promptsRender(name string, data any) (string, error) {
	return prompts.Render(name, data)
}
```

Add the imports: `"github.com/MoonCodeMaster/AIOS/internal/config"` and `"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestReplShipCalls -v`
Expected: PASS — the injected `ShipFn` fires; the real `runAutopilotShip` is not exercised in this unit test.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl.go internal/cli/repl_test.go
git commit -m "feat(repl): /ship hands spec to autopilot via decompose+run"
```

---

### Task 15: Context-window summarization

**Files:**
- Modify: `internal/specgen/pipeline.go`
- Modify: `internal/specgen/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/specgen/pipeline_test.go`:

```go
func TestGenerateSummarizesPriorTurnsAboveThreshold(t *testing.T) {
	bigBody := strings.Repeat("A", 250*1024) // 250 KB > 200 KB threshold
	prior := []Turn{{UserMessage: "old", FinalSpec: bigBody}}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}
	_, err := Generate(context.Background(), Input{
		UserRequest: "new",
		PriorTurns:  prior,
		Claude:      claude,
		Codex:       codex,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// The draft prompt sent to Claude should contain a summarization marker
	// and NOT the full 250 KB body.
	stage1Prompt := claude.Received[0].Prompt
	if strings.Contains(stage1Prompt, bigBody) {
		t.Fatalf("draft prompt contained full prior turn body — summarization did not trigger")
	}
	if !strings.Contains(stage1Prompt, "[prior context summarized:") {
		t.Fatalf("draft prompt missing summarization marker; got first 200 chars: %s", stage1Prompt[:200])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/specgen/ -run TestGenerateSummarizes`
Expected: FAIL — full body is currently included.

- [ ] **Step 3: Implement the threshold**

In `internal/specgen/pipeline.go`, replace the construction of `priorForTmpl`:

```go
	priorForTmpl := make([]map[string]string, len(in.PriorTurns))
	for i, t := range in.PriorTurns {
		priorForTmpl[i] = map[string]string{"UserMessage": t.UserMessage}
	}
```

with:

```go
	priorForTmpl := buildPriorContext(in.PriorTurns)
```

Add this helper at the bottom of `pipeline.go`:

```go
// priorContextThreshold is the byte cap for prior-turn material before
// summarization kicks in. Conservative starting value; tunable in code.
const priorContextThreshold = 200 * 1024

// buildPriorContext flattens prior turns for the draft template. If the
// total accumulated size exceeds priorContextThreshold, older turns are
// collapsed into one "summary" entry.
func buildPriorContext(turns []Turn) []map[string]string {
	total := 0
	for _, t := range turns {
		total += len(t.UserMessage) + len(t.FinalSpec)
	}
	if total <= priorContextThreshold {
		out := make([]map[string]string, len(turns))
		for i, t := range turns {
			out[i] = map[string]string{"UserMessage": t.UserMessage}
		}
		return out
	}
	// Keep the most recent turn verbatim, collapse the rest.
	if len(turns) == 0 {
		return nil
	}
	last := turns[len(turns)-1]
	older := turns[:len(turns)-1]
	collapsed := fmt.Sprintf("[prior context summarized: %d earlier turns over %d bytes — see .aios/sessions/<id>/session.json for full history]",
		len(older), total-len(last.UserMessage)-len(last.FinalSpec))
	return []map[string]string{
		{"UserMessage": collapsed},
		{"UserMessage": last.UserMessage},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/specgen/ -v`
Expected: all pipeline tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/specgen/pipeline.go internal/specgen/pipeline_test.go
git commit -m "feat(specgen): summarize prior turns above 200 KB threshold"
```

---

## Phase 3 — wiring, resume, integration, docs

### Task 16: Wire root command to launch REPL on bare invocation

**Files:**
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Read current root**

Run: `cat internal/cli/root.go`

- [ ] **Step 2: Modify the root command**

Replace `internal/cli/root.go` with:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/spf13/cobra"
)

// Version is stamped by GoReleaser at build time.
var Version = "dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "aios",
		Short:   "AIOS — dual-AI project orchestrator",
		Long:    "Drives Claude CLI and Codex CLI as a coder↔reviewer pair over a spec-driven task queue.",
		Version: Version,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare `aios` (no subcommand, no positional args) launches the REPL.
			if len(args) > 0 {
				return cmd.Help()
			}
			return launchRepl(cmd.Context())
		},
	}
	root.PersistentFlags().String("config", ".aios/config.toml", "path to AIOS config")
	root.PersistentFlags().String("log-level", "info", "log level: debug|info|warn|error")
	root.PersistentFlags().Bool("dry-run", false, "print actions without calling engines or writing git")
	root.PersistentFlags().Bool("yolo", false, "on full success, merge aios/staging into base branch")
	root.PersistentFlags().String("resume", "", "resume an REPL session (empty = latest, or pass a session ID)")
	root.AddCommand(newStatusCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newNewCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newAutopilotCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newArchitectCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newDuelCmd())
	root.AddCommand(newCostCmd())
	root.AddCommand(newLessonsCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newMCPCmd())
	return root
}

// launchRepl boots a Repl with real engines and stdio, then runs it.
func launchRepl(ctx context.Context) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	r := &Repl{
		Wd:           wd,
		In:           os.Stdin,
		Out:          os.Stdout,
		Claude:       &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:        &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		ClaudeBinary: cfg.Engines.Claude.Binary,
		CodexBinary:  cfg.Engines.Codex.Binary,
	}
	return r.Run(ctx)
}
```

- [ ] **Step 3: Verify the build still works**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 4: Verify existing subcommands are not broken**

Run: `go test ./internal/cli/...`
Expected: all existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): bare `aios` launches the interactive REPL"
```

---

### Task 17: Resume from existing session

**Files:**
- Modify: `internal/cli/repl.go`
- Modify: `internal/cli/repl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/repl_test.go`:

```go
func TestReplResumeRestoresTurns(t *testing.T) {
	wd := t.TempDir()
	sessionID := "2026-04-26T10-00-00"
	sessionDir := filepath.Join(wd, ".aios", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := &Session{
		ID:         sessionID,
		Created:    time.Now().UTC(),
		SessionDir: sessionDir,
		SpecPath:   filepath.Join(wd, ".aios", "project.md"),
		Turns: []SessionTurn{
			{Timestamp: time.Now().UTC(), UserMessage: "first", SpecAfter: "OLD_SPEC", RunID: "r1"},
		},
	}
	if err := prior.Save(); err != nil {
		t.Fatal(err)
	}
	// .aios/project.md must exist for the resume to be coherent.
	if err := os.WriteFile(prior.SpecPath, []byte("OLD_SPEC"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Start a Repl with ResumeID = "" (latest) and verify it picked up the prior session.
	stdout := &bytes.Buffer{}
	r := &Repl{
		Wd:       wd,
		In:       strings.NewReader("/exit\n"),
		Out:      stdout,
		Claude:   &engine.FakeEngine{Name_: "claude"},
		Codex:    &engine.FakeEngine{Name_: "codex"},
		ResumeID: "", // latest
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.session == nil || r.session.ID != sessionID {
		t.Fatalf("session not restored; got %+v", r.session)
	}
	if len(r.session.Turns) != 1 || r.session.Turns[0].UserMessage != "first" {
		t.Fatalf("turn history not restored; got %+v", r.session.Turns)
	}
}
```

(Add `"time"` to imports if missing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestReplResume`
Expected: FAIL — `ResumeID` field does not exist.

- [ ] **Step 3: Add resume support**

In `internal/cli/repl.go`:

1. Add to the `Repl` struct:

```go
	ResumeID string // empty = use LatestSession; specific ID = LoadSession(<id>)
```

2. Modify `bootSession` to honor `ResumeID`:

```go
func (r *Repl) bootSession() error {
	if r.session != nil {
		return nil
	}
	sessionsDir := filepath.Join(r.Wd, ".aios", "sessions")
	switch {
	case r.ResumeID != "":
		s, err := LoadSession(filepath.Join(sessionsDir, r.ResumeID))
		if err != nil {
			return fmt.Errorf("resume %s: %w", r.ResumeID, err)
		}
		r.session = s
		fmt.Fprintf(r.Out, "resumed session %s (%d prior turns)\n", s.ID, len(s.Turns))
		return nil
	default:
		// Try latest if any sessions exist.
		if _, err := os.Stat(sessionsDir); err == nil {
			if s, err := LatestSession(sessionsDir); err == nil {
				r.session = s
				fmt.Fprintf(r.Out, "resumed session %s (%d prior turns)\n", s.ID, len(s.Turns))
				return nil
			}
		}
	}
	// Fresh session.
	id := NewSessionID()
	r.session = &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: filepath.Join(sessionsDir, id),
		SpecPath:   filepath.Join(r.Wd, ".aios", "project.md"),
	}
	return r.session.Save()
}
```

**Note:** This auto-resumes the latest session by default, which is what the spec wants for the `--resume` flag with no value. To start fresh users can `/clear` after resume, or we can add an explicit `--new-session` flag in a follow-up (out of scope).

3. In `internal/cli/root.go` `launchRepl`, pull the resume flag from cobra and pass it:

```go
	resumeID, _ := flags(ctx).GetString("resume")
	r.ResumeID = resumeID
```

(Use `cmd.Flags().GetString("resume")` directly inside `RunE` and pass into `launchRepl(ctx, resumeID)` if cleaner. Adjust `launchRepl` signature accordingly.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestReplResume -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl.go internal/cli/root.go internal/cli/repl_test.go
git commit -m "feat(repl): --resume restores latest session or named session"
```

---

### Task 18: Integration test (end-to-end with fakes)

**Files:**
- Create: `internal/cli/repl_integration_test.go`

- [ ] **Step 1: Write the integration test**

Create `internal/cli/repl_integration_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

func TestReplEndToEnd_HappyShip(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}

	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "DRAFT_A"}, {Text: "POLISHED_FINAL"},
	}}
	codex := &engine.FakeEngine{Name_: "codex", Script: []engine.InvokeResponse{
		{Text: "DRAFT_B"}, {Text: "MERGED"},
	}}

	shipped := false
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader("design a thing\n\n/ship\n\n"),
		Out:    &bytes.Buffer{},
		Claude: claude,
		Codex:  codex,
		ShipFn: func(_ context.Context, w string) error {
			// Verify the spec is on disk before /ship runs.
			data, err := os.ReadFile(filepath.Join(w, ".aios", "project.md"))
			if err != nil {
				return err
			}
			if string(data) != "POLISHED_FINAL" {
				t.Fatalf("ShipFn saw spec = %q, want POLISHED_FINAL", data)
			}
			shipped = true
			return nil
		},
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !shipped {
		t.Fatalf("ShipFn was not called")
	}

	// Verify intermediate drafts are on disk in the run dir.
	runs, err := os.ReadDir(filepath.Join(wd, ".aios", "runs"))
	if err != nil {
		t.Fatalf("read runs dir: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want exactly 1 run dir, got %d", len(runs))
	}
	for _, name := range []string{"draft-claude.md", "draft-codex.md", "merged.md", "final.md", "stages.json"} {
		if _, err := os.Stat(filepath.Join(wd, ".aios", "runs", runs[0].Name(), "specgen", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	// Verify session.json captured the turn.
	sessions, err := os.ReadDir(filepath.Join(wd, ".aios", "sessions"))
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	sessRaw, err := os.ReadFile(filepath.Join(wd, ".aios", "sessions", sessions[0].Name(), "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var got Session
	if err := json.Unmarshal(sessRaw, &got); err != nil {
		t.Fatalf("unmarshal session.json: %v", err)
	}
	if len(got.Turns) != 1 || got.Turns[0].UserMessage != "design a thing" {
		t.Fatalf("session turns wrong: %+v", got.Turns)
	}
}

func TestReplEndToEnd_MergeFailureWarnsAndContinues(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Codex first call (draft) succeeds; second call (merge) errors.
	// FakeEngine has no "fail this call" knob, so use scriptedEngine pattern from
	// pipeline_test or wrap: easiest is a small local engine that fails on the Nth call.
	codex := &engine.FailOnCallEngine{
		Name_: "codex",
		Script: []engine.InvokeResponse{
			{Text: "DRAFT_B_long_enough_to_be_picked_as_fallback_when_merge_fails"},
		},
		FailOnCall: 2, // second call (the merge) returns error
	}
	claude := &engine.FakeEngine{Name_: "claude", Script: []engine.InvokeResponse{
		{Text: "A"},          // short draft, loses the longer-draft fallback
		{Text: "POLISHED"},   // polish the fallback
	}}

	out := &bytes.Buffer{}
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader("idea\n\n/exit\n"),
		Out:    out,
		Claude: claude,
		Codex:  codex,
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Merge step failed") {
		t.Fatalf("expected merge-fallback warning in stdout; got: %s", out.String())
	}
	specBody, _ := os.ReadFile(filepath.Join(wd, ".aios", "project.md"))
	if string(specBody) != "POLISHED" {
		t.Fatalf("spec = %q, want POLISHED", specBody)
	}
}
```

- [ ] **Step 2: Add `FailOnCallEngine` to the engine package**

Append to `internal/engine/fake.go`:

```go
// FailOnCallEngine returns scripted responses; the call numbered FailOnCall
// (1-based) returns an error instead. Used by REPL integration tests to
// exercise mid-pipeline failure paths.
type FailOnCallEngine struct {
	Name_      string
	Script     []InvokeResponse
	Received   []InvokeRequest
	FailOnCall int // 1-based; 0 = never fail
	calls      int
}

func (f *FailOnCallEngine) Name() string { return f.Name_ }

func (f *FailOnCallEngine) Invoke(_ context.Context, req InvokeRequest) (*InvokeResponse, error) {
	f.Received = append(f.Received, req)
	f.calls++
	if f.calls == f.FailOnCall {
		return nil, errors.New("FailOnCallEngine: scripted failure")
	}
	if f.calls-1 >= len(f.Script) {
		// Past scripted responses and not the failure call: synthesize a placeholder.
		return &InvokeResponse{Text: ""}, nil
	}
	r := f.Script[f.calls-1]
	return &r, nil
}
```

- [ ] **Step 3: Run integration tests**

Run: `go test ./internal/cli/ -run TestReplEndToEnd -v`
Expected: both tests PASS.

- [ ] **Step 4: Run the full test suite to catch regressions**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/repl_integration_test.go internal/engine/fake.go
git commit -m "test(repl): end-to-end happy + merge-failure integration coverage"
```

---

### Task 19: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`

- [ ] **Step 1: Read README and architecture**

Run: `head -80 README.md && echo '---' && cat docs/architecture.md | head -60`

- [ ] **Step 2: Update README**

Edit `README.md`:

1. Add to the command index (near other top-level commands):

```markdown
- [`aios`](#interactive-mode) — interactive entry point: each turn produces a unified spec via the dual-AI pipeline; `/ship` hands off to autopilot
```

2. Add a new section after the existing top-level command sections:

```markdown
## Interactive mode

Run `aios` with no subcommand to launch an interactive session. Same usage shape as `claude` or `codex` CLIs.

```
$ aios
aios — type a requirement, blank line to submit. /help for commands.
> build a CLI for syncing notebooks across machines
>
  · draft-claude …
  · draft-codex …
  · merge …
  · polish …
Spec updated (84 lines). /show to view, /ship to implement, or refine with another message.
> tighten the section on conflict resolution
>
  · ...
> /ship
shipping spec to autopilot…
```

Each turn runs a deterministic 4-stage dual-AI pipeline:

1. Claude generates draft A.
2. Codex generates draft B (in parallel).
3. Codex merges A and B into one spec, with initial polish.
4. Claude does secondary refinement on the merged spec.

The final spec is written to `.aios/project.md`. The four intermediate drafts are saved under `.aios/runs/<run-id>/specgen/` for inspection.

**Slash commands:** `/show`, `/clear`, `/help`, `/ship`, `/exit`.

**Resume:** `aios --resume` (latest session) or `aios --resume <session-id>` (specific). Sessions are persisted under `.aios/sessions/<id>/session.json`.

**Failure handling:** if one drafter fails, the surviving engine produces the spec alone with a warning. If the merge step fails, the longer of the two drafts becomes the merged version. If polish fails, the merged version is the final. Dual-CLI absence (either Claude or Codex missing from PATH) refuses to launch — run `aios doctor` to diagnose.

`aios new` and `aios architect` remain available for users who want the legacy single-shot or three-blueprint flows.
```

- [ ] **Step 3: Update architecture doc**

Append to `docs/architecture.md`:

```markdown

## Interactive entry point and specgen pipeline

`aios` (no subcommand) launches `internal/cli/repl.Repl`, an interactive turn loop. Each user message is dispatched to `internal/specgen.Generate`, which runs four sequential stages with stages 1 and 2 in parallel:

```
        ┌─ Claude draft A ──┐
user ──>│                   ├─> Codex merge ──> Claude polish ──> .aios/project.md
        └─ Codex draft B  ──┘
```

Intermediate drafts and per-stage timing/token metrics are persisted under `.aios/runs/<run-id>/specgen/`. Session state (turn history, current spec path) is persisted under `.aios/sessions/<id>/session.json` after every turn so a crashed REPL is resumable via `aios --resume`. The `/ship` slash command reuses the existing autopilot path: decompose the spec on disk into task files, then `aios run --autopilot --merge`.

Partial-failure fallbacks (one drafter dead, merge fails, polish fails) are handled inside `Generate` and surfaced to the REPL via `Output.Warnings`. No automatic retries.
```

- [ ] **Step 4: Verify the docs render**

Run: `head -120 README.md`
Expected: new section visible, no syntax errors.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/architecture.md
git commit -m "docs: interactive aios entry point and specgen pipeline"
```

---

### Task 20: End-to-end build, lint, and full-suite verification

**Files:** none (verification only)

- [ ] **Step 1: Build the binary**

Run: `go build -o bin/aios ./cmd/aios`
Expected: clean build.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: all tests pass. (TestRebaseReviewRejects is known flaky — retry once if it fails.)

- [ ] **Step 3: Manual smoke test**

Run: `cd /tmp && rm -rf aios-smoke && mkdir aios-smoke && cd aios-smoke && /Users/ldy/Desktop/project/AIOS/bin/aios --help`
Expected: help text shows the existing subcommands; bare `aios` is documented as launching the REPL.

(Don't actually launch the REPL in smoke — it would call real Claude/Codex CLIs and cost tokens. The integration tests cover the real flow with fakes.)

- [ ] **Step 4: Final commit if anything was tweaked**

If steps 1-3 forced a fix, commit it; otherwise skip.

---

## Self-review

**Spec coverage:** every section of `docs/superpowers/specs/2026-04-26-aios-interactive-specgen-design.md` maps to at least one task:

- Goal and non-goals → covered by overall plan structure.
- Product surface (REPL, slash commands, quiet pipeline output) → Tasks 11, 12.
- 4-stage pipeline (parallel 1+2, sequential 3→4, role assignment, on-disk outputs) → Tasks 1–8.
- REPL session shape and turn loop → Tasks 9, 11, 17.
- `/ship` handoff → Task 14.
- Refinement context window → Task 15.
- Error handling table → Tasks 6, 7, 8 (specgen-side); Task 13 (CLI-missing); Task 14 (`/ship` failure delegated to autopilot).
- Testing (unit, integration, reused fake harness) → embedded in every task; integration in Task 18.
- Files to create/modify → all listed in plan.

**Placeholder scan:** searched for "TBD", "TODO", "implement later", "add appropriate" — none present in the executable parts. Tasks 12 and 19 reference earlier tasks ("implementation already exists from Task 11", "see Task 23" in Task 6 — wait, that note in Task 6 referred to docs being updated in Task 23, but the docs task is now Task 19) — fix below.

**Type consistency:** `Session.SessionDir`, `Session.SpecPath`, `Repl.ShipFn`, `Repl.ResumeID`, `specgen.Input.OnStageStart`, `StageMetric.Skipped`, `StageMetric.Fallback` — all referenced names match across tasks.

**Inline fix:** in Task 6 Step 1 the inline note says "documented in Task 23 (docs)" — the docs task is Task 19, not 23. Updating now.
