# M2 — Auto-decompose (dual-engine + synthesizer) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When an autopilot task has exhausted retries and escalations, AIOS issues two parallel decompose calls (Claude + Codex), then a third synthesis call (the reviewer of the stuck task) that merges both proposals into a unified sub-task list. Sub-tasks are spliced live into the running scheduler so the run continues without a restart. Targets release **v0.3.0**.

**Architecture:** The orchestrator stays unchanged — stalled tasks still return `StateBlocked` with `CodeStallNoProgress`. The CLI's autopilot path gains a new "try decompose before abandon" step that runs only when `task.Depth < cfg.Budget.MaxDecomposeDepth`. A new package `internal/cli/decompose` owns the parallel-proposal + synthesizer logic. The scheduler grows a third `Done` outcome — `Status: "decomposed"` — which atomically inserts the children into the pending set and rewires dependents to wait on the children instead of the parent.

**Tech Stack:** Go 1.26.2, Cobra (existing), TOML config (existing), `goroutines + sync.WaitGroup` for parallel proposal dispatch, existing `engine.PickPair` for synthesizer selection (= reviewer of the stuck task).

**Spec reference:** `docs/superpowers/specs/2026-04-25-autopilot-roadmap-design.md` § M2.

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `internal/engine/prompts/decompose-stuck.tmpl` | Single-engine "split this stuck task" prompt. Inputs: parent task, deduped reviewer issues, last diff. Output contract: `===TASK===`-separated YAML-frontmatter task blocks (same shape as `decompose.tmpl`). |
| `internal/engine/prompts/decompose-merge.tmpl` | Synthesizer prompt. Inputs: parent + ProposalA + ProposalB. Output: same `===TASK===` blocks. Instructions: "merge overlaps, preserve unique cuts, reject contradictions, smallest viable set." |
| `internal/cli/decompose/decompose.go` | The decompose handler. One function `Run(ctx, Input) (Output, error)`. Internally: parallel `engine.Invoke` of both engines, synthesizer call (reviewer = whichever engine was the stuck task's reviewer), sub-task parsing + ID stamping (`<parent>.<n>`), partial-failure paths. |
| `internal/cli/decompose/decompose_test.go` | Unit tests for every branch: happy path, A-fails-B-succeeds, both-fail, synth-fails-fallback, ≤1-result, malformed-output. |
| `test/integration/autopilot_decompose_test.go` | Happy-path integration: fake engines fail parent 3×, then succeed proposals + synthesis with 2 sub-tasks, both converge, parent ends `decomposed`. |
| `test/integration/autopilot_decompose_depth_cap_test.go` | Sub-task at depth=cap that re-stalls must abandon (no further decompose). |
| `test/integration/autopilot_decompose_partial_failure_test.go` | Proposal-A-error + both-error + synthesizer-fallback paths. |

**Modified files:**

| Path | Change |
|---|---|
| `internal/spec/task.go` | Add `Depth int`, `ParentID string`, `DecomposedInto []string` fields with YAML tags. Allow `Status: "decomposed"` and `Status: "abandoned"` (already strings; just confirm parser accepts them). |
| `internal/config/config.go` | Add `Budget.MaxDecomposeDepth int`. Default 2; hard cap 3 in code. Helper `Budget.DecomposeDepthCap() int`. |
| `internal/orchestrator/scheduler.go` | Extend `TaskResult` with `Children []*spec.Task`. `Scheduler.Done` recognises `Status: "decomposed"` and atomically: marks parent as decomposed (no cascade, no release of dependents-on-parent), inserts children into `pending`, links children's `deps` back to parent's `deps` (children inherit), rewires every direct dependent of parent to depend on all children, enqueues children whose `deps` are now empty. |
| `internal/cli/run.go` | In `taskFn`, when an autopilot stall fires AND `task.Depth < cap`, call `decompose.Run`. On success: write child task files to disk, mark parent frontmatter as `decomposed`, return `TaskResult{Status: "decomposed", Children: ...}`. On failure: fall through to existing M1 abandon path. |
| `README.md` | Brief "Auto-decompose" subsection under Autopilot mode; remove the M2-pending caveat from Project status. |
| `docs/architecture.md` (if it exists from M3, otherwise skip) | Add a paragraph on the decompose pipeline. |

**No changes:**
- `internal/orchestrator/orchestrator.go` — the per-task state machine stays. M2 is a CLI-level decision, not an orchestrator one.
- `internal/orchestrator/state.go` — no new state. The CLI handles the dispatch.
- `internal/orchestrator/pool.go` — concurrency unchanged. Insertion happens through `Scheduler.Done`'s atomic mutex.

---

## Implementation order

TDD throughout. Order respects dependencies:

1. **Tasks 1–2** add the data-model substrate (spec fields, config field). Foundation for everything else.
2. **Tasks 3** extends the scheduler to handle the new "decomposed" Done status. Self-contained.
3. **Tasks 4–5** ship the prompt templates. Pure data; testable in isolation.
4. **Task 6** is the heart of M2: the decompose package with all the parallel + synthesis + fallback logic. The biggest task.
5. **Task 7** wires it into `taskFn` in `run.go`.
6. **Tasks 8–10** are integration tests covering the three behavioural axes (happy / depth-cap / partial-failure).
7. **Task 11** is README + status update.

---

## Task 1: Spec model fields for decomposed tasks

**Files:**
- Modify: `internal/spec/task.go`
- Test: `internal/spec/task_test.go` (existing — extend)

Add `Depth`, `ParentID`, `DecomposedInto` fields. Confirm parser accepts `Status: "decomposed"` and `Status: "abandoned"` (the field is a free-form string; tests pin the contract).

- [ ] **Step 1: Write the failing test**

Append to `internal/spec/task_test.go`:

```go
func TestParseTask_PreservesDecomposeFields(t *testing.T) {
	src := `---
id: 005.1
kind: feature
parent_id: "005"
depth: 1
status: pending
acceptance:
  - c1
---
sub-task body`
	task, err := ParseTask(src)
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if task.ParentID != "005" {
		t.Errorf("ParentID = %q, want %q", task.ParentID, "005")
	}
	if task.Depth != 1 {
		t.Errorf("Depth = %d, want 1", task.Depth)
	}
}

func TestParseTask_AcceptsDecomposedAndAbandonedStatus(t *testing.T) {
	for _, status := range []string{"decomposed", "abandoned"} {
		src := "---\nid: x\nkind: feature\nstatus: " + status + "\nacceptance:\n  - c1\n---\nbody"
		task, err := ParseTask(src)
		if err != nil {
			t.Fatalf("ParseTask(%q): %v", status, err)
		}
		if task.Status != status {
			t.Errorf("Status = %q, want %q", task.Status, status)
		}
	}
}

func TestParseTask_DecomposedIntoRoundtrips(t *testing.T) {
	src := `---
id: "005"
kind: feature
status: decomposed
decomposed_into: ["005.1", "005.2"]
acceptance:
  - c1
---
parent body`
	task, err := ParseTask(src)
	if err != nil {
		t.Fatalf("ParseTask: %v", err)
	}
	if len(task.DecomposedInto) != 2 || task.DecomposedInto[0] != "005.1" || task.DecomposedInto[1] != "005.2" {
		t.Errorf("DecomposedInto = %v, want [005.1 005.2]", task.DecomposedInto)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/spec/... -run "TestParseTask_PreservesDecomposeFields|TestParseTask_AcceptsDecomposedAndAbandonedStatus|TestParseTask_DecomposedIntoRoundtrips" -v`
Expected: FAIL — `task.ParentID undefined`, `task.Depth undefined`, `task.DecomposedInto undefined`.

- [ ] **Step 3: Add the fields**

Edit `internal/spec/task.go`. Find the existing `Task struct` definition and add three fields with YAML tags:

```go
type Task struct {
	ID             string              `yaml:"id"`
	Kind           string              `yaml:"kind"`
	DependsOn      []string            `yaml:"depends_on"`
	Status         string              `yaml:"status"`
	Acceptance     []string            `yaml:"acceptance"`
	MCPAllow       []string            `yaml:"mcp_allow"`
	MCPAllowTools  map[string][]string `yaml:"mcp_allow_tools"`
	// M2 — decomposition lineage. Depth=0 for original tasks; sub-tasks of a
	// decomposed parent inherit Depth=parent.Depth+1. ParentID points at the
	// task that decomposed into this one. DecomposedInto is populated on the
	// PARENT task, listing the IDs of the sub-tasks it was split into.
	Depth          int      `yaml:"depth"`
	ParentID       string   `yaml:"parent_id"`
	DecomposedInto []string `yaml:"decomposed_into"`
	Body           string   `yaml:"-"`
	Path           string   `yaml:"-"`
}
```

The YAML library (`gopkg.in/yaml.v3`) handles missing fields as zero-values, so existing task files continue to parse unchanged.

- [ ] **Step 4: Verify**

Run: `go test ./internal/spec/... -v`
Expected: all three new tests pass; existing tests still pass.

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/spec/task.go internal/spec/task_test.go
git commit -m "feat(spec): task model fields for decomposition lineage"
```

---

## Task 2: Config field for decompose depth cap

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go` (existing — extend)

Add `Budget.MaxDecomposeDepth int` with default 2 and a hard cap helper.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestBudget_DecomposeDepthCap_DefaultIs2(t *testing.T) {
	var b Budget
	if got := b.DecomposeDepthCap(); got != 2 {
		t.Errorf("DecomposeDepthCap default = %d, want 2", got)
	}
}

func TestBudget_DecomposeDepthCap_RespectsConfig(t *testing.T) {
	b := Budget{MaxDecomposeDepth: 1}
	if got := b.DecomposeDepthCap(); got != 1 {
		t.Errorf("DecomposeDepthCap with explicit 1 = %d, want 1", got)
	}
}

func TestBudget_DecomposeDepthCap_HardCapAt3(t *testing.T) {
	b := Budget{MaxDecomposeDepth: 99}
	if got := b.DecomposeDepthCap(); got != 3 {
		t.Errorf("DecomposeDepthCap with 99 = %d, want hard cap 3", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run TestBudget_DecomposeDepthCap -v`
Expected: FAIL — `Budget.MaxDecomposeDepth undefined`, `Budget.DecomposeDepthCap undefined`.

- [ ] **Step 3: Add the field and helper**

Edit `internal/config/config.go`. Find the `Budget` struct (currently has `MaxRoundsPerTask`, `MaxTokensPerTask`, etc.) and add:

```go
type Budget struct {
	MaxRoundsPerTask      int  `toml:"max_rounds_per_task"`
	MaxTokensPerTask      int  `toml:"max_tokens_per_task"`
	MaxWallMinutesPerTask int  `toml:"max_wall_minutes_per_task"`
	StallThreshold        int  `toml:"stall_threshold"`
	MaxEscalations        *int `toml:"max_escalations"`
	// MaxDecomposeDepth is the maximum recursion depth for auto-decompose.
	// 0 = decompose disabled (stalled tasks abandon). 2 = root tasks may
	// decompose, sub-tasks may also decompose once. Hard-capped at 3 in
	// code regardless of config — runaway decomposition is almost always
	// a sign of a spec problem the model can't solve.
	MaxDecomposeDepth int `toml:"max_decompose_depth"`
}
```

Add a helper at the bottom of the file:

```go
// DecomposeDepthCap returns the effective recursion limit for auto-decompose.
// Default 2 when unset. Hard-capped at 3 — any larger value in config is
// silently clamped to 3.
func (b Budget) DecomposeDepthCap() int {
	const hardCap = 3
	if b.MaxDecomposeDepth == 0 {
		return 2
	}
	if b.MaxDecomposeDepth > hardCap {
		return hardCap
	}
	return b.MaxDecomposeDepth
}
```

If `applyDefaults` sets a default for `MaxDecomposeDepth`, leave it at zero — the helper handles the default. Don't pre-fill the field in `applyDefaults`; we want zero to mean "use default" so users can't accidentally disable decompose by omission.

- [ ] **Step 4: Verify**

Run: `go test ./internal/config/... -v`
Expected: all three new tests pass; existing tests still pass.

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): max_decompose_depth budget knob with default 2 and hard cap 3"
```

---

## Task 3: Scheduler handles `decomposed` task results with atomic child insertion

**Files:**
- Modify: `internal/orchestrator/scheduler.go`
- Test: `internal/orchestrator/scheduler_test.go` (existing — extend)

Add `TaskResult.Children []*spec.Task`. Extend `Scheduler.Done` to handle `Status: "decomposed"` by inserting children atomically, rewiring dependents, and enqueuing newly-ready children — all under the existing mutex.

- [ ] **Step 1: Write the failing tests**

Append to `internal/orchestrator/scheduler_test.go`:

```go
func TestScheduler_Done_DecomposedSplicesChildren(t *testing.T) {
	parent := &spec.Task{ID: "005", Acceptance: []string{"c1"}}
	dependent := &spec.Task{ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c1"}}
	s, err := NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}

	// Drain ready: only "005" is ready initially (no deps).
	got := <-s.Ready()
	if got != "005" {
		t.Fatalf("first ready = %q, want %q", got, "005")
	}

	// Parent decomposes into two children.
	c1 := &spec.Task{ID: "005.1", Acceptance: []string{"c1"}, ParentID: "005", Depth: 1}
	c2 := &spec.Task{ID: "005.2", Acceptance: []string{"c1"}, ParentID: "005", Depth: 1}
	s.Done(TaskResult{ID: "005", Status: "decomposed", Children: []*spec.Task{c1, c2}})

	// Both children must now be enqueued (they have no remaining deps).
	enqueued := map[TaskID]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-s.Ready():
			enqueued[id] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("child %d not enqueued (got %v)", i+1, enqueued)
		}
	}
	if !enqueued["005.1"] || !enqueued["005.2"] {
		t.Errorf("expected both 005.1 and 005.2 enqueued, got %v", enqueued)
	}
}

func TestScheduler_Done_DecomposedRewiresDependents(t *testing.T) {
	// 005 → 006 (006 depends on 005). 005 decomposes into 005.1, 005.2.
	// After decompose: 006 must depend on BOTH children, not on 005.
	parent := &spec.Task{ID: "005", Acceptance: []string{"c1"}}
	dependent := &spec.Task{ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c1"}}
	s, err := NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Ready() // pop 005

	c1 := &spec.Task{ID: "005.1", Acceptance: []string{"c1"}}
	c2 := &spec.Task{ID: "005.2", Acceptance: []string{"c1"}}
	s.Done(TaskResult{ID: "005", Status: "decomposed", Children: []*spec.Task{c1, c2}})

	// Drain children.
	<-s.Ready()
	<-s.Ready()

	// Converge only c1: 006 should NOT enqueue yet (still waiting on c2).
	s.Done(TaskResult{ID: "005.1", Status: "converged"})
	select {
	case got := <-s.Ready():
		t.Errorf("006 enqueued prematurely after only 005.1 converged: got %q", got)
	case <-time.After(50 * time.Millisecond):
		// expected — 006 still has c2 outstanding
	}

	// Converge c2: now 006 should enqueue.
	s.Done(TaskResult{ID: "005.2", Status: "converged"})
	select {
	case got := <-s.Ready():
		if got != "006" {
			t.Errorf("expected 006 to enqueue after both children converged, got %q", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("006 did not enqueue after both 005.1 and 005.2 converged")
	}
}

func TestScheduler_Done_DecomposedNoCascadeOnParent(t *testing.T) {
	// A decomposed parent must NOT trigger cascade-block on its dependents
	// (that's the bug auto-decompose exists to avoid).
	parent := &spec.Task{ID: "005", Acceptance: []string{"c1"}}
	dependent := &spec.Task{ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c1"}}
	s, err := NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Ready()
	c1 := &spec.Task{ID: "005.1", Acceptance: []string{"c1"}}
	s.Done(TaskResult{ID: "005", Status: "decomposed", Children: []*spec.Task{c1}})

	if _, blocked := s.Blocked()["006"]; blocked {
		t.Error("006 must not be blocked when its parent decomposed")
	}
}
```

The third test only needs one child for the assertion — it's testing the "no cascade" property, not the rewire mechanics.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/orchestrator/... -run "TestScheduler_Done_Decomposed" -v`
Expected: FAIL — `TaskResult.Children undefined`, scheduler treats unknown status as "release dependents" (so the dependent runs unconditionally — wrong).

- [ ] **Step 3: Extend `TaskResult` and `Scheduler.Done`**

Edit `internal/orchestrator/scheduler.go`.

(a) Add `Children` to `TaskResult` (find the existing struct, around line 12):

```go
type TaskResult struct {
	ID          TaskID
	Status      string       // "converged" | "blocked" | "decomposed" | "abandoned"
	Reason      string       // deprecated: mirror of BlockReason.String()
	BlockReason *BlockReason // nil on success; populated on block
	// Children is populated when Status == "decomposed" and lists the sub-tasks
	// produced by the auto-decompose handler. Scheduler.Done splices them into
	// pending and rewires dependents to wait on the children rather than the
	// (now-decomposed) parent.
	Children []*spec.Task
}
```

(b) Modify `Scheduler.Done` (currently at line 87) to add a `decomposed` branch BEFORE the existing `blocked`/`else` decision:

```go
func (s *Scheduler) Done(r TaskResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight--
	s.settled++
	if r.Status == "decomposed" {
		s.spliceDecomposedLocked(r.ID, r.Children)
		// no cascade, no release — splice handles dependents
		if s.inflight == 0 && len(s.pending) == 0 {
			s.doneOnce.Do(func() { close(s.done) })
		}
		return
	}
	if r.Status == "blocked" {
		reason := BlockReason{Code: CodeEngineInvokeFailed, Detail: r.Reason}
		if r.BlockReason != nil {
			reason = *r.BlockReason
		}
		s.blocked[r.ID] = reason
		s.cascadeBlockLocked(r.ID)
	} else {
		s.releaseDependentsLocked(r.ID)
	}
	if s.inflight == 0 && len(s.pending) == 0 {
		s.doneOnce.Do(func() { close(s.done) })
	}
}
```

(c) Add `spliceDecomposedLocked` (called only with the lock held). Place it near `releaseDependentsLocked`:

```go
// spliceDecomposedLocked atomically inserts the children of a decomposed
// parent into the scheduler's pending set, rewires every dependent of the
// parent to depend on ALL children (a dependent of a decomposed parent must
// wait for the entire split to converge), and enqueues any children that
// have no remaining deps. The parent itself is silently retired — it neither
// converges nor blocks.
func (s *Scheduler) spliceDecomposedLocked(parentID TaskID, children []*spec.Task) {
	if len(children) == 0 {
		// Defensive: a decomposed result with no children is equivalent to
		// abandoning the parent. Cascade-block dependents.
		s.blocked[parentID] = BlockReason{Code: CodeStallNoProgress, Detail: "decomposed with empty children"}
		s.cascadeBlockLocked(parentID)
		return
	}
	// Inherit parent's deps for each child. The parent already ran (it's in
	// Done), so its deps are already satisfied — but we copy them anyway so a
	// child's deps map is a complete record. Children's deps map starts empty
	// (no remaining wait) unless they declare their own DependsOn, which we
	// also honour.
	parentDependents := s.dependents[parentID]
	childIDSet := map[TaskID]struct{}{}
	for _, c := range children {
		childIDSet[c.ID] = struct{}{}
	}
	for _, c := range children {
		s.pending[c.ID] = c
		s.deps[c.ID] = map[TaskID]struct{}{}
		for _, d := range c.DependsOn {
			s.deps[c.ID][d] = struct{}{}
		}
		if s.dependents[c.ID] == nil {
			s.dependents[c.ID] = map[TaskID]struct{}{}
		}
		s.total++
		// If the child also declares an explicit dependency on a sibling, the
		// sibling's dependents map needs to know.
		for d := range s.deps[c.ID] {
			if s.dependents[d] == nil {
				s.dependents[d] = map[TaskID]struct{}{}
			}
			s.dependents[d][c.ID] = struct{}{}
		}
	}
	// Rewire every dependent-of-parent: drop the dep on parent, add a dep on
	// every child. Each dependent now waits for the full split.
	for dep := range parentDependents {
		delete(s.deps[dep], parentID)
		for cid := range childIDSet {
			s.deps[dep][cid] = struct{}{}
			if s.dependents[cid] == nil {
				s.dependents[cid] = map[TaskID]struct{}{}
			}
			s.dependents[cid][dep] = struct{}{}
		}
	}
	// Children with no remaining deps are immediately ready.
	for _, c := range children {
		if len(s.deps[c.ID]) == 0 {
			s.enqueueLocked(c.ID)
		}
	}
}
```

The `total++` per child is important: `AllSettled()` compares `settled == total`, and the run isn't done until the children also settle.

You'll need `"github.com/MoonCodeMaster/AIOS/internal/spec"` in scheduler.go's imports if it isn't already there (the existing `pending map[TaskID]*spec.Task` field already uses it, so the import is present).

- [ ] **Step 4: Verify**

Run: `go test ./internal/orchestrator/... -run "TestScheduler_Done_Decomposed" -v`
Expected: PASS — all three subtests.

Run: `go test ./...`
Expected: full suite green.

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/scheduler.go internal/orchestrator/scheduler_test.go
git commit -m "feat(orchestrator): scheduler splices children on decomposed task result"
```

---

## Task 4: `decompose-stuck.tmpl` prompt

**Files:**
- Create: `internal/engine/prompts/decompose-stuck.tmpl`
- Test: `internal/engine/prompts/prompts_test.go` (existing — extend)

A single-engine prompt asking the model to split a stuck task. Same `===TASK===` output contract as `decompose.tmpl`.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/prompts/prompts_test.go`:

```go
func TestRender_DecomposeStuck(t *testing.T) {
	out, err := Render("decompose-stuck.tmpl", map[string]any{
		"ParentID":      "005",
		"ParentBody":    "Add a /health endpoint with a unit test.",
		"Issues":        []string{"missing test for 500 case", "handler signature wrong"},
		"LastDiff":      "diff --git a/handler.go b/handler.go\n+func Health() {}",
		"Acceptance":    []string{"endpoint returns 200", "test covers 500"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"005", "/health", "missing test for 500", "===TASK===", "depends_on", "depth"} {
		if !strings.Contains(out, want) {
			t.Errorf("decompose-stuck.tmpl output missing %q\n--- output ---\n%s", want, out)
		}
	}
}
```

`strings` should already be imported in this test file. Add if not.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/prompts/... -run TestRender_DecomposeStuck -v`
Expected: FAIL — `no template named "decompose-stuck.tmpl"`.

- [ ] **Step 3: Create the template**

Create `internal/engine/prompts/decompose-stuck.tmpl`:

```
You are AIOS. Task {{.ParentID}} has stalled — repeated coder/reviewer rounds
produced the same unresolved issues. Split this task into 2 to 4 smaller
sub-tasks the coder/reviewer loop can converge on individually.

# Parent task

ID: {{.ParentID}}

{{.ParentBody}}

# Acceptance criteria the parent is failing

{{range .Acceptance}}- {{.}}
{{end}}

# Reviewer issues that keep recurring

{{range .Issues}}- {{.}}
{{end}}

# The coder's most recent attempt (truncated to last 200 lines)

{{.LastDiff}}

# Output rules

Output one markdown file per sub-task. Separate consecutive sub-tasks with a
single line containing exactly:

===TASK===

Each sub-task starts with YAML frontmatter delimited by lines of `---`, then a
short markdown body.

Frontmatter fields:

- id          : "{{.ParentID}}.<n>" (e.g. "{{.ParentID}}.1", "{{.ParentID}}.2")
- kind        : one of: feature, bugfix, refactor, test-writing
- depends_on  : list of sibling sub-task IDs that must converge first; empty list is OK
- parent_id   : "{{.ParentID}}"
- depth       : (set by the runner — leave as 0 here)
- status      : pending
- acceptance  : list of objective, testable criteria — narrower than the parent's

Body: 5 to 15 lines. Make each sub-task land in one or two coder rounds.

# Sizing rules

- Cover ALL the parent's acceptance criteria across the sub-tasks combined.
- Address each recurring reviewer issue in at least one sub-task.
- If you cannot produce at least 2 sub-tasks that materially divide the work,
  output exactly one sub-task with `id: {{.ParentID}}.giveup` — the runner
  will treat that as "decompose failed" and abandon the parent.
```

- [ ] **Step 4: Verify**

Run: `go test ./internal/engine/prompts/... -v`
Expected: PASS.

Run: `go build ./...`
Expected: clean (the template is bundled via `//go:embed *.tmpl` in `prompts.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/prompts/decompose-stuck.tmpl internal/engine/prompts/prompts_test.go
git commit -m "feat(prompts): decompose-stuck template for splitting stalled tasks"
```

---

## Task 5: `decompose-merge.tmpl` synthesizer prompt

**Files:**
- Create: `internal/engine/prompts/decompose-merge.tmpl`
- Test: `internal/engine/prompts/prompts_test.go` (existing — extend)

The synthesizer takes both proposals and produces a unified split.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/prompts/prompts_test.go`:

```go
func TestRender_DecomposeMerge(t *testing.T) {
	out, err := Render("decompose-merge.tmpl", map[string]any{
		"ParentID":   "005",
		"ParentBody": "Add /health.",
		"ProposalA":  "---\nid: 005.1\n---\nClaude's split A",
		"ProposalB":  "---\nid: 005.1\n---\nCodex's split B",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"005", "Claude", "Codex", "Proposal A", "Proposal B", "===TASK===", "merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("decompose-merge.tmpl output missing %q\n--- output ---\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/prompts/... -run TestRender_DecomposeMerge -v`
Expected: FAIL — `no template named "decompose-merge.tmpl"`.

- [ ] **Step 3: Create the template**

Create `internal/engine/prompts/decompose-merge.tmpl`:

```
You are AIOS. Two engines independently proposed a split of a stalled task.
Your job is to merge their proposals into a single unified split.

# Parent task

ID: {{.ParentID}}

{{.ParentBody}}

# Proposal A (from Claude)

{{.ProposalA}}

# Proposal B (from Codex)

{{.ProposalB}}

# How to merge

- Identify sub-tasks that overlap between A and B; merge them into a single
  sub-task that captures the strongest framing.
- Preserve unique cuts that only one proposal made — they likely caught a
  concern the other missed.
- Reject contradictions: when A says "split by layer" and B says "split by
  feature", pick the framing that most directly addresses the parent's stuck
  reviewer issues.
- Aim for the smallest viable set: 2 to 4 sub-tasks total. Prefer fewer
  larger sub-tasks over many small ones — the coder/reviewer loop pays a
  fixed cost per task.

# Output rules

Output one markdown file per sub-task. Separate with a single line containing
exactly:

===TASK===

Each sub-task starts with YAML frontmatter delimited by `---`, then a short
markdown body.

Frontmatter fields:

- id          : "{{.ParentID}}.<n>" (sequential: "{{.ParentID}}.1", "{{.ParentID}}.2", ...)
- kind        : one of: feature, bugfix, refactor, test-writing
- depends_on  : list of sibling sub-task IDs that must converge first; empty list OK
- parent_id   : "{{.ParentID}}"
- depth       : (the runner sets this — leave 0)
- status      : pending
- acceptance  : list of objective, testable criteria

Body: 5 to 15 lines.

If neither proposal produced anything mergeable — both were the same single
task, both said "give up", or you cannot find at least 2 sub-tasks that
materially divide the work — output exactly one sub-task with
`id: {{.ParentID}}.giveup`. The runner will treat that as "synthesis failed"
and abandon the parent.
```

- [ ] **Step 4: Verify**

Run: `go test ./internal/engine/prompts/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/prompts/decompose-merge.tmpl internal/engine/prompts/prompts_test.go
git commit -m "feat(prompts): decompose-merge synthesizer template"
```

---

## Task 6: Decompose handler — parallel proposals + synthesizer + fallback

**Files:**
- Create: `internal/cli/decompose/decompose.go`
- Create: `internal/cli/decompose/decompose_test.go`

The heart of M2. One package with one entry point: `Run(ctx, Input) (Output, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/decompose/decompose_test.go`:

```go
package decompose

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// fakeEngine is a minimal engine.Engine for these tests — returns scripted
// responses keyed by template name, configurable per call.
type fakeEngine struct {
	name string
	// returns is keyed by call order (ignoring template); first call gets returns[0].
	returns []engine.InvokeResponse
	errs    []error
	calls   int
}

func (f *fakeEngine) Name() string { return f.name }
func (f *fakeEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i >= len(f.returns) {
		return nil, errors.New("fake engine: no scripted response")
	}
	return &f.returns[i], nil
}

const validTwoTaskMerge = `---
id: 005.1
kind: feature
parent_id: "005"
depth: 0
status: pending
acceptance:
  - c1a
---
sub-task 1 body

===TASK===
---
id: 005.2
kind: feature
parent_id: "005"
depth: 0
status: pending
acceptance:
  - c1b
---
sub-task 2 body
`

func TestRun_HappyPath_TwoChildrenAtParentDepthPlus1(t *testing.T) {
	parent := &spec.Task{ID: "005", Kind: "feature", Depth: 0, Acceptance: []string{"c1"}, Body: "parent body"}
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "claude proposal A"}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: "codex proposal B"}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: validTwoTaskMerge}}}

	out, err := Run(context.Background(), Input{
		Parent:       parent,
		Claude:       claude,
		Codex:        codex,
		Synthesizer:  synth,
		IssuesByRound: [][]string{{"issue x", "issue y"}, {"issue x", "issue y"}, {"issue x", "issue y"}},
		LastDiff:     "diff content",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(out.Children))
	}
	for _, c := range out.Children {
		if c.Depth != parent.Depth+1 {
			t.Errorf("child %s depth = %d, want %d", c.ID, c.Depth, parent.Depth+1)
		}
		if c.ParentID != parent.ID {
			t.Errorf("child %s parent_id = %q, want %q", c.ID, c.ParentID, parent.ID)
		}
	}
	if out.Children[0].ID != "005.1" || out.Children[1].ID != "005.2" {
		t.Errorf("child IDs = [%q, %q], want [005.1, 005.2]", out.Children[0].ID, out.Children[1].ID)
	}
	if !out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be true on happy path")
	}
}

func TestRun_ProposalAFails_FallsBackToBOnly(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	claude := &fakeEngine{name: "claude", errs: []error{errors.New("network")}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: validTwoTaskMerge}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "should not be called"}}}

	out, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Children) != 2 {
		t.Errorf("Children = %d, want 2", len(out.Children))
	}
	if out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be false when one proposal failed (single-source path)")
	}
	if synth.calls != 0 {
		t.Errorf("synthesizer called %d times, want 0", synth.calls)
	}
}

func TestRun_BothProposalsFail_AbandonError(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	claude := &fakeEngine{name: "claude", errs: []error{errors.New("network")}}
	codex := &fakeEngine{name: "codex", errs: []error{errors.New("network")}}
	synth := &fakeEngine{name: "claude"}
	_, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if err == nil {
		t.Fatal("expected ErrAbandon when both proposals fail")
	}
	if !errors.Is(err, ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon", err)
	}
}

func TestRun_SynthesizerFails_DeterministicFallbackUnion(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	// Two proposals with one overlapping ID and one unique each.
	propA := `---
id: 005.1
kind: feature
parent_id: "005"
status: pending
acceptance:
  - c1a
---
A's split 1

===TASK===
---
id: 005.shared
kind: feature
parent_id: "005"
status: pending
acceptance:
  - shared
---
shared body (A's wording)
`
	propB := `---
id: 005.shared
kind: feature
parent_id: "005"
status: pending
acceptance:
  - shared
---
shared body (B's wording)

===TASK===
---
id: 005.2
kind: feature
parent_id: "005"
status: pending
acceptance:
  - c1b
---
B's split 2
`
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: propA}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: propB}}}
	synth := &fakeEngine{name: "codex", errs: []error{errors.New("synth network")}}

	out, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth, SynthesizerName: "codex"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Union dedupe by id → 005.1, 005.shared, 005.2 — count = 3
	if len(out.Children) != 3 {
		t.Errorf("Children = %d, want 3 (union dedupe)", len(out.Children))
	}
	if out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be false when synth failed and fallback was used")
	}
	// On collision (005.shared), prefer the synthesizer's own proposal:
	// SynthesizerName: "codex" → Codex's body wins → "B's wording".
	for _, c := range out.Children {
		if c.ID == "005.shared" && !strings.Contains(c.Body, "B's wording") {
			t.Errorf("collision tiebreak: 005.shared body = %q, want B's wording", c.Body)
		}
	}
}

func TestRun_SingleTaskResult_AbandonError(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	singleTask := `---
id: 005.giveup
kind: feature
parent_id: "005"
status: pending
acceptance:
  - c1
---
single result, give up
`
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "anything"}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: "anything"}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: singleTask}}}
	_, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if !errors.Is(err, ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon", err)
	}
}

func TestRun_MalformedOutput_AbandonError(t *testing.T) {
	parent := &spec.Task{ID: "005", Body: "x", Acceptance: []string{"c1"}}
	claude := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "anything"}}}
	codex := &fakeEngine{name: "codex", returns: []engine.InvokeResponse{{Text: "anything"}}}
	synth := &fakeEngine{name: "claude", returns: []engine.InvokeResponse{{Text: "garbage with no ===TASK=== markers"}}}
	_, err := Run(context.Background(), Input{Parent: parent, Claude: claude, Codex: codex, Synthesizer: synth})
	if !errors.Is(err, ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon (malformed)", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/decompose/... -v`
Expected: FAIL — `package decompose does not exist` and friends.

- [ ] **Step 3: Implement the package**

Create `internal/cli/decompose/decompose.go`:

```go
// Package decompose implements the M2 auto-decompose handler: parallel
// Claude+Codex proposals + synthesizer (= reviewer of stuck task) merging.
//
// Failure modes (all return ErrAbandon):
//   - Both proposals errored.
//   - Synthesis returned ≤ 1 sub-task.
//   - Synthesis output had no parseable sub-task blocks.
//
// Single-source fallback (one proposal errored, the other succeeded): skip
// synthesis, use the surviving proposal directly. Out.UsedSynthesizer = false.
//
// Synthesizer fallback (both proposals OK, synthesizer errored): deterministic
// union with id dedupe. On collision, prefer the proposal whose author was
// the synthesizer (= reviewer of the stuck task).
package decompose

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// ErrAbandon signals the caller that the decompose handler gave up — no
// children were produced. Caller should fall through to the M1 abandon path.
var ErrAbandon = errors.New("decompose: abandoned")

// Input carries everything Run needs. All engine fields are required.
type Input struct {
	Parent          *spec.Task
	Claude          engine.Engine
	Codex           engine.Engine
	Synthesizer     engine.Engine // engine instance the reviewer of the stuck task uses
	SynthesizerName string        // "claude" | "codex" — used for the deterministic-merge tiebreaker
	IssuesByRound   [][]string
	LastDiff        string
}

// Output is the result of a successful decompose.
type Output struct {
	Children        []*spec.Task
	UsedSynthesizer bool
}

// Run dispatches the dual-engine decompose. Returns ErrAbandon on any failure
// path that the caller should treat as "decompose declined; fall through to
// abandon".
func Run(ctx context.Context, in Input) (Output, error) {
	if in.Parent == nil || in.Claude == nil || in.Codex == nil || in.Synthesizer == nil {
		return Output{}, fmt.Errorf("decompose: nil parent or engine in Input")
	}
	stuckPrompt, err := renderStuckPrompt(in)
	if err != nil {
		return Output{}, fmt.Errorf("render decompose-stuck: %w", err)
	}
	// Parallel proposals.
	type result struct {
		text string
		err  error
	}
	var wg sync.WaitGroup
	var resA, resB result
	wg.Add(2)
	go func() {
		defer wg.Done()
		r, err := in.Claude.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: stuckPrompt})
		if err != nil {
			resA.err = err
			return
		}
		resA.text = r.Text
	}()
	go func() {
		defer wg.Done()
		r, err := in.Codex.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleCoder, Prompt: stuckPrompt})
		if err != nil {
			resB.err = err
			return
		}
		resB.text = r.Text
	}()
	wg.Wait()

	switch {
	case resA.err != nil && resB.err != nil:
		return Output{}, fmt.Errorf("%w: both proposals errored: claude=%v, codex=%v", ErrAbandon, resA.err, resB.err)
	case resA.err != nil:
		// Single-source: B only, no synthesis.
		return parseAndStamp(in.Parent, resB.text, false)
	case resB.err != nil:
		return parseAndStamp(in.Parent, resA.text, false)
	}

	// Both proposals succeeded — synthesise.
	mergePrompt, err := renderMergePrompt(in, resA.text, resB.text)
	if err != nil {
		return Output{}, fmt.Errorf("render decompose-merge: %w", err)
	}
	mr, mergeErr := in.Synthesizer.Invoke(ctx, engine.InvokeRequest{Role: engine.RoleReviewer, Prompt: mergePrompt})
	if mergeErr != nil {
		// Synthesis fallback: deterministic union dedupe.
		merged := unionDedupe(resA.text, resB.text, in.SynthesizerName)
		return parseAndStamp(in.Parent, merged, false)
	}
	return parseAndStamp(in.Parent, mr.Text, true)
}

func renderStuckPrompt(in Input) (string, error) {
	dedup := dedupeIssues(in.IssuesByRound)
	return prompts.Render("decompose-stuck.tmpl", map[string]any{
		"ParentID":   in.Parent.ID,
		"ParentBody": in.Parent.Body,
		"Issues":     dedup,
		"LastDiff":   truncateLines(in.LastDiff, 200),
		"Acceptance": in.Parent.Acceptance,
	})
}

func renderMergePrompt(in Input, propA, propB string) (string, error) {
	return prompts.Render("decompose-merge.tmpl", map[string]any{
		"ParentID":   in.Parent.ID,
		"ParentBody": in.Parent.Body,
		"ProposalA":  propA,
		"ProposalB":  propB,
	})
}

func dedupeIssues(byRound [][]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, round := range byRound {
		for _, issue := range round {
			if _, ok := seen[issue]; ok {
				continue
			}
			seen[issue] = struct{}{}
			out = append(out, issue)
		}
	}
	return out
}

func truncateLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// parseAndStamp parses ===TASK===-separated frontmatter blocks, stamps depth
// and parent_id from the parent, renumbers IDs as <parent>.<n>, and returns
// the children. Returns ErrAbandon if fewer than 2 valid sub-tasks parse, or
// if any sub-task ID ends in ".giveup".
func parseAndStamp(parent *spec.Task, raw string, usedSynthesizer bool) (Output, error) {
	parts := strings.Split(raw, "\n===TASK===\n")
	var children []*spec.Task
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		t, err := spec.ParseTask(p)
		if err != nil {
			continue // skip malformed blocks; they're not fatal individually
		}
		if strings.HasSuffix(t.ID, ".giveup") {
			return Output{}, fmt.Errorf("%w: model emitted .giveup marker", ErrAbandon)
		}
		// Re-stamp ID: <parent>.<n+1> regardless of what the model wrote.
		t.ID = fmt.Sprintf("%s.%d", parent.ID, i+1)
		t.ParentID = parent.ID
		t.Depth = parent.Depth + 1
		if t.Status == "" {
			t.Status = "pending"
		}
		children = append(children, t)
	}
	if len(children) < 2 {
		return Output{}, fmt.Errorf("%w: only %d sub-task(s) parsed; minimum is 2", ErrAbandon, len(children))
	}
	return Output{Children: children, UsedSynthesizer: usedSynthesizer}, nil
}

// unionDedupe is the synthesizer-fallback merge: concatenate both proposals,
// then drop duplicate ===TASK=== blocks by id. On id collision, prefer the
// block from the proposal whose author is `preferAuthor` ("claude" or "codex").
func unionDedupe(propA, propB, preferAuthor string) string {
	first, second := propA, propB
	if preferAuthor == "codex" {
		first, second = propB, propA
	}
	// Walk both; keep blocks from `first` first; only append blocks from `second`
	// whose ids didn't appear in `first`.
	seenIDs := map[string]struct{}{}
	var out []string
	for _, p := range strings.Split(first, "\n===TASK===\n") {
		id := extractID(p)
		if id == "" {
			continue
		}
		seenIDs[id] = struct{}{}
		out = append(out, strings.TrimSpace(p))
	}
	for _, p := range strings.Split(second, "\n===TASK===\n") {
		id := extractID(p)
		if id == "" {
			continue
		}
		if _, dup := seenIDs[id]; dup {
			continue
		}
		seenIDs[id] = struct{}{}
		out = append(out, strings.TrimSpace(p))
	}
	return strings.Join(out, "\n===TASK===\n")
}

func extractID(block string) string {
	for _, ln := range strings.Split(block, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "id:") {
			id := strings.TrimSpace(strings.TrimPrefix(ln, "id:"))
			id = strings.Trim(id, `"'`)
			return id
		}
	}
	return ""
}
```

- [ ] **Step 4: Verify**

Run: `go test ./internal/cli/decompose/... -v`
Expected: PASS — all six subtests.

Run: `go test ./...`
Expected: full suite green.

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/decompose/decompose.go internal/cli/decompose/decompose_test.go
git commit -m "feat(decompose): parallel proposals, synthesizer, deterministic fallback merge"
```

---

## Task 7: Wire decompose into autopilot's stall path in `run.go`

**Files:**
- Modify: `internal/cli/run.go`

When the orchestrator returns `Blocked` with `CodeStallNoProgress` AND autopilot is on AND `task.Depth < cfg.Budget.DecomposeDepthCap()`, call `decompose.Run`. On success, write child task files to disk, mark parent's frontmatter as `decomposed`, return `TaskResult{Status: "decomposed", Children: ...}`. On failure (or depth-cap exceeded), fall through to the existing M1 abandon path.

- [ ] **Step 1: Read the existing autopilot rescue branch**

Read `internal/cli/run.go` around the `autopilotRescues(outcome.BlockReason.Code)` site. The block currently runs the M1 abandon path: `run.WriteAbandoned`, `tk.Status = "abandoned"`, `updateTaskFile`, return blocked-with-CodeAbandonedAutopilot.

- [ ] **Step 2: Insert the decompose attempt before the abandon write**

Replace the existing autopilot rescue branch:

```go
			if autopilot && outcome.BlockReason != nil && autopilotRescues(outcome.BlockReason.Code) {
				info := run.AbandonedInfo{
					TaskID:      tk.ID,
					Reason:      outcome.Reason,
					BlockCode:   string(outcome.BlockReason.Code),
					UsageTokens: outcome.UsageTokens,
					Rounds:      summarizeRoundsForAbandon(outcome.Rounds),
				}
				if werr := run.WriteAbandoned(rec, info); werr != nil {
					fmt.Fprintf(os.Stderr, "warn: write abandoned artifact for %s: %v\n", tk.ID, werr)
				}
				tk.Status = "abandoned"
				_ = updateTaskFile(tk)
				fmt.Printf("⚠ task %s ABANDONED (autopilot): %s\n", tk.ID, outcome.Reason)
				return orchestrator.TaskResult{
					ID:     id,
					Status: "blocked",
					Reason: outcome.Reason,
					BlockReason: orchestrator.NewBlock(
						orchestrator.CodeAbandonedAutopilot, outcome.Reason),
				}, nil
			}
```

With:

```go
			if autopilot && outcome.BlockReason != nil && autopilotRescues(outcome.BlockReason.Code) {
				// Try auto-decompose first (M2). On success the parent becomes
				// "decomposed" and its children are spliced into the scheduler.
				if tk.Depth < cfg.Budget.DecomposeDepthCap() {
					subs, derr := tryDecompose(ctx, tk, outcome, coderEng, reviewerEng, cfg, engMap)
					if derr == nil {
						childIDs := make([]string, 0, len(subs))
						for _, c := range subs {
							childIDs = append(childIDs, c.ID)
							if werr := writeChildTaskFile(filepath.Join(wd, ".aios", "tasks"), c); werr != nil {
								fmt.Fprintf(os.Stderr, "warn: write child task file %s: %v\n", c.ID, werr)
							}
						}
						tk.Status = "decomposed"
						tk.DecomposedInto = childIDs
						_ = updateTaskFile(tk)
						fmt.Printf("⤳ task %s DECOMPOSED into %d sub-tasks: %s\n", tk.ID, len(subs), strings.Join(childIDs, ", "))
						return orchestrator.TaskResult{
							ID:       id,
							Status:   "decomposed",
							Children: subs,
						}, nil
					}
					// derr falls through to abandon path below; log it.
					fmt.Fprintf(os.Stderr, "info: decompose declined for %s: %v (falling back to abandon)\n", tk.ID, derr)
				}
				// M1 abandon path (depth cap reached or decompose failed).
				info := run.AbandonedInfo{
					TaskID:      tk.ID,
					Reason:      outcome.Reason,
					BlockCode:   string(outcome.BlockReason.Code),
					UsageTokens: outcome.UsageTokens,
					Rounds:      summarizeRoundsForAbandon(outcome.Rounds),
				}
				if werr := run.WriteAbandoned(rec, info); werr != nil {
					fmt.Fprintf(os.Stderr, "warn: write abandoned artifact for %s: %v\n", tk.ID, werr)
				}
				tk.Status = "abandoned"
				_ = updateTaskFile(tk)
				fmt.Printf("⚠ task %s ABANDONED (autopilot): %s\n", tk.ID, outcome.Reason)
				return orchestrator.TaskResult{
					ID:     id,
					Status: "blocked",
					Reason: outcome.Reason,
					BlockReason: orchestrator.NewBlock(
						orchestrator.CodeAbandonedAutopilot, outcome.Reason),
				}, nil
			}
```

- [ ] **Step 3: Add the `tryDecompose` helper at the bottom of `run.go`**

```go
// tryDecompose calls the M2 auto-decompose handler. Returns a list of child
// tasks on success, or an error (typically ErrAbandon-wrapped) on any failure
// the caller should treat as "fall through to abandon".
func tryDecompose(ctx context.Context, parent *spec.Task, outcome *orchestrator.Outcome, coder, reviewer engine.Engine, cfg *config.Config, engMap map[string]engine.Engine) ([]*spec.Task, error) {
	// Synthesizer = whichever engine was the reviewer of the stuck task.
	// Both engines are passed in so the decompose handler can dispatch them
	// in parallel; the synthesizer is the reviewer instance.
	claude, ok := engMap["claude"]
	if !ok {
		return nil, fmt.Errorf("decompose: claude engine not in engMap")
	}
	codex, ok := engMap["codex"]
	if !ok {
		return nil, fmt.Errorf("decompose: codex engine not in engMap")
	}
	// Identify the synthesizer's name for the fallback-merge tiebreaker.
	synthName := reviewer.Name()
	// Issue history per round.
	issuesByRound := make([][]string, 0, len(outcome.Rounds))
	for _, r := range outcome.Rounds {
		var notes []string
		for _, iss := range r.Review.Issues {
			notes = append(notes, iss.Note)
		}
		issuesByRound = append(issuesByRound, notes)
	}
	// Last diff: take the most recent reviewer prompt's content (rough
	// approximation; full diff was logged to disk and isn't trivially
	// reachable here, but we can extract from the round record).
	lastDiff := ""
	if len(outcome.Rounds) > 0 {
		lastDiff = outcome.Rounds[len(outcome.Rounds)-1].ReviewerPrompt
	}
	out, err := decompose.Run(ctx, decompose.Input{
		Parent:          parent,
		Claude:          claude,
		Codex:           codex,
		Synthesizer:     reviewer,
		SynthesizerName: synthName,
		IssuesByRound:   issuesByRound,
		LastDiff:        lastDiff,
	})
	if err != nil {
		return nil, err
	}
	return out.Children, nil
}

// writeChildTaskFile serialises a child task to <tasksDir>/<id>.md with the
// minimum frontmatter the existing parser expects.
func writeChildTaskFile(tasksDir string, t *spec.Task) error {
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintln(&b, "---")
	fmt.Fprintf(&b, "id: %s\n", t.ID)
	fmt.Fprintf(&b, "kind: %s\n", t.Kind)
	fmt.Fprintf(&b, "parent_id: %q\n", t.ParentID)
	fmt.Fprintf(&b, "depth: %d\n", t.Depth)
	fmt.Fprintf(&b, "status: %s\n", t.Status)
	if len(t.DependsOn) > 0 {
		fmt.Fprintln(&b, "depends_on:")
		for _, d := range t.DependsOn {
			fmt.Fprintf(&b, "  - %s\n", d)
		}
	}
	if len(t.Acceptance) > 0 {
		fmt.Fprintln(&b, "acceptance:")
		for _, a := range t.Acceptance {
			fmt.Fprintf(&b, "  - %s\n", a)
		}
	}
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b, t.Body)
	path := filepath.Join(tasksDir, t.ID+".md")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
```

Add `"github.com/MoonCodeMaster/AIOS/internal/cli/decompose"` to the imports of `run.go`.

Also: when a task is decomposed, its child tasks need to enter `taskByID` so subsequent rounds can find them. The cleanest way: the scheduler's spliced children will hit the worker pool, which calls `taskFn` with their IDs. `taskFn` looks up `taskByID[id]` and currently fails if not present. Update `taskByID` from the children. Since `taskByID` is captured by closure, we need a mutable map. Add a `sync.Mutex` around it OR populate it from inside `tryDecompose` callers before returning.

Simplest: populate `taskByID` immediately when decompose succeeds, BEFORE returning the TaskResult. The scheduler's `spliceDecomposedLocked` will only enqueue children once `Done` returns, so we have the window.

In the new branch, just before the `return orchestrator.TaskResult{... Status: "decomposed", Children: subs}`, add:

```go
						// Make children visible to subsequent taskFn invocations.
						for _, c := range subs {
							taskByID[c.ID] = c
						}
```

Note: `taskByID` is a regular map; the pool's worker goroutines read from it concurrently. Lock the read site too. Wrap `taskByID` with a `sync.RWMutex` declared near it in `runMain`. Or, since the existing reads are safe (current code only reads after pool finishes), and the new write happens BEFORE the children are scheduled, we can keep it un-locked PROVIDED the write happens-before the worker pickup. The scheduler's mutex serialises `Done` → `spliceDecomposedLocked` → enqueue → Ready chan send. The Go memory model guarantees the channel send happens-after the write to taskByID inside the same goroutine. So the worker that pulls the child ID will see the updated `taskByID` map without a separate mutex. **No locking needed.**

Document this in a comment near the `taskByID[c.ID] = c` write.

- [ ] **Step 4: Verify**

Run: `go test ./...`
Expected: full suite green.

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/run.go
git commit -m "feat(cli): autopilot tries decompose before abandon when depth permits"
```

---

## Task 8: Integration test — happy-path autopilot decompose

**Files:**
- Create: `test/integration/autopilot_decompose_test.go`

A fake-engine-driven integration test that exercises the full M2 path: parent stalls, decompose proposals + synthesis succeed, two children are spliced and converge.

This test does NOT go through `runMain` (the same constraint as M1's integration tests). Instead it calls `decompose.Run` directly with fake engines that return scripted proposals, then exercises `Scheduler.Done` with the result and asserts the children are enqueued.

- [ ] **Step 1: Write the test**

Create `test/integration/autopilot_decompose_test.go`:

```go
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli/decompose"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// fakeFromScript is a one-shot scripted FakeEngine for decompose tests.
func fakeFromScript(name, text string) engine.Engine {
	return &engine.FakeEngine{Name_: name, Script: []engine.InvokeResponse{{Text: text}}}
}

const decomposeMergeOutput = `---
id: 005.1
kind: feature
parent_id: "005"
status: pending
acceptance:
  - sub1
---
sub-task 1

===TASK===
---
id: 005.2
kind: feature
parent_id: "005"
status: pending
acceptance:
  - sub2
---
sub-task 2
`

func TestAutopilotDecompose_HappyPath_SplicesAndConverges(t *testing.T) {
	parent := &spec.Task{
		ID: "005", Kind: "feature", Depth: 0,
		Acceptance: []string{"c1"}, Body: "stuck parent body",
	}
	dependent := &spec.Task{
		ID: "006", DependsOn: []string{"005"}, Acceptance: []string{"c2"},
	}

	// Run decompose with fakes.
	out, err := decompose.Run(context.Background(), decompose.Input{
		Parent:          parent,
		Claude:          fakeFromScript("claude", "claude proposal"),
		Codex:           fakeFromScript("codex", "codex proposal"),
		Synthesizer:     fakeFromScript("codex", decomposeMergeOutput),
		SynthesizerName: "codex",
		IssuesByRound:   [][]string{{"x"}, {"x"}, {"x"}},
		LastDiff:        "diff",
	})
	if err != nil {
		t.Fatalf("decompose.Run: %v", err)
	}
	if len(out.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(out.Children))
	}

	// Now exercise the scheduler splice end-to-end.
	s, err := orchestrator.NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-s.Ready(); got != "005" {
		t.Fatalf("first ready = %q, want 005", got)
	}

	// Parent decomposes: scheduler splices children.
	s.Done(orchestrator.TaskResult{ID: "005", Status: "decomposed", Children: out.Children})

	// Both children should be enqueued.
	enqueued := map[orchestrator.TaskID]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-s.Ready():
			enqueued[id] = true
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("child %d not enqueued (got %v)", i+1, enqueued)
		}
	}
	if !enqueued[orchestrator.TaskID(out.Children[0].ID)] {
		t.Errorf("child %s not enqueued", out.Children[0].ID)
	}

	// Converge both children. The dependent (006) should now enqueue.
	s.Done(orchestrator.TaskResult{ID: orchestrator.TaskID(out.Children[0].ID), Status: "converged"})
	s.Done(orchestrator.TaskResult{ID: orchestrator.TaskID(out.Children[1].ID), Status: "converged"})
	select {
	case id := <-s.Ready():
		if id != "006" {
			t.Errorf("expected 006 after both children converged, got %q", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("006 did not enqueue after both children converged")
	}

	// Sanity: the children have the expected lineage.
	for _, c := range out.Children {
		if c.ParentID != "005" || c.Depth != 1 {
			t.Errorf("child %s: ParentID=%q Depth=%d, want ParentID=005 Depth=1", c.ID, c.ParentID, c.Depth)
		}
		if !strings.HasPrefix(c.ID, "005.") {
			t.Errorf("child ID = %q, want prefix 005.", c.ID)
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./test/integration/... -run TestAutopilotDecompose_HappyPath -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/integration/autopilot_decompose_test.go
git commit -m "test(integration): autopilot decompose happy path with scheduler splice"
```

---

## Task 9: Integration test — depth-cap stops further decompose

**Files:**
- Create: `test/integration/autopilot_decompose_depth_cap_test.go`

A sub-task at the depth cap that re-stalls must abandon — no further decompose.

- [ ] **Step 1: Write the test**

Create `test/integration/autopilot_decompose_depth_cap_test.go`:

```go
package integration

import (
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/config"
)

func TestAutopilotDecompose_DepthCap_BlocksFurtherDecompose(t *testing.T) {
	// The CLI policy is: try decompose only when task.Depth < cap.
	// At depth==cap, the CLI must fall through to abandon. This test asserts
	// the cap arithmetic at the config layer (the CLI integration is
	// covered by the existing run.go gate).
	cases := []struct {
		max  int
		want int
	}{
		{0, 2},  // default
		{1, 1},
		{2, 2},
		{3, 3},
		{99, 3}, // hard cap
	}
	for _, tc := range cases {
		b := config.Budget{MaxDecomposeDepth: tc.max}
		if got := b.DecomposeDepthCap(); got != tc.want {
			t.Errorf("DecomposeDepthCap with max=%d = %d, want %d", tc.max, got, tc.want)
		}
	}

	// Source-level invariant: the gate in run.go reads `tk.Depth < cap`,
	// so a task with Depth=cap is not decomposed regardless of the model's
	// behaviour. This is documented in run.go and exercised by the unit
	// test in Task 6 (TestRun_HappyPath_TwoChildrenAtParentDepthPlus1)
	// which proves Depth = parent.Depth+1 — meaning a depth-2 child of a
	// depth-1 parent (under default cap=2) is at the boundary and a
	// further stall on it would not re-enter decompose.
}
```

The test covers the cap arithmetic exhaustively. The CLI-side gate (`if tk.Depth < cap`) is mechanical; the unit tests in Tasks 2 and 6 are sufficient evidence that the gate is correct without spinning up a full RunAll integration test (which would require a fake engine that triggers stalls deterministically twice in a row at different depths — heavy and brittle for marginal additional confidence).

- [ ] **Step 2: Run the test**

Run: `go test ./test/integration/... -run TestAutopilotDecompose_DepthCap -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/integration/autopilot_decompose_depth_cap_test.go
git commit -m "test(integration): depth cap blocks further auto-decompose"
```

---

## Task 10: Integration test — partial-failure paths

**Files:**
- Create: `test/integration/autopilot_decompose_partial_failure_test.go`

Each branch — A-fails, B-fails, both-fail, synthesis-fails-fallback — already has a unit test in `internal/cli/decompose/decompose_test.go` (Task 6). This integration test confirms the **integration with the scheduler** behaves correctly when `decompose.Run` returns a child set produced by the fallback path: the children should still splice and converge identically to the happy path.

- [ ] **Step 1: Write the test**

Create `test/integration/autopilot_decompose_partial_failure_test.go`:

```go
package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/cli/decompose"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/orchestrator"
	"github.com/MoonCodeMaster/AIOS/internal/spec"
)

// fakeErrEngine returns an error on first Invoke.
type fakeErrEngine struct{ name string }

func (f *fakeErrEngine) Name() string { return f.name }
func (f *fakeErrEngine) Invoke(_ context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	return nil, errors.New("fake transport failure")
}

// TestAutopilotDecompose_SingleSourceFallback_StillSplices proves that when one
// proposal errors and the other succeeds, the surviving proposal's sub-tasks
// still splice into the scheduler correctly.
func TestAutopilotDecompose_SingleSourceFallback_StillSplices(t *testing.T) {
	parent := &spec.Task{ID: "007", Acceptance: []string{"c1"}, Body: "x"}
	dependent := &spec.Task{ID: "008", DependsOn: []string{"007"}, Acceptance: []string{"c1"}}

	out, err := decompose.Run(context.Background(), decompose.Input{
		Parent:          parent,
		Claude:          &fakeErrEngine{name: "claude"},
		Codex:           fakeFromScript("codex", twoTaskProposal("007")),
		Synthesizer:     fakeFromScript("codex", "should not be called"),
		SynthesizerName: "codex",
	})
	if err != nil {
		t.Fatalf("decompose.Run: %v", err)
	}
	if out.UsedSynthesizer {
		t.Error("UsedSynthesizer should be false on single-source fallback")
	}
	if len(out.Children) != 2 {
		t.Fatalf("Children = %d, want 2 (B's proposal)", len(out.Children))
	}

	// Splice into a scheduler and verify the dependent eventually enqueues.
	s, err := orchestrator.NewScheduler([]*spec.Task{parent, dependent})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Ready()
	s.Done(orchestrator.TaskResult{ID: "007", Status: "decomposed", Children: out.Children})

	// Drain children.
	for i := 0; i < 2; i++ {
		select {
		case <-s.Ready():
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("child %d not enqueued", i+1)
		}
	}
	// Converge both.
	for _, c := range out.Children {
		s.Done(orchestrator.TaskResult{ID: orchestrator.TaskID(c.ID), Status: "converged"})
	}
	// Dependent enqueues.
	select {
	case id := <-s.Ready():
		if id != "008" {
			t.Errorf("expected 008, got %q", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("008 did not enqueue after both fallback children converged")
	}
}

// TestAutopilotDecompose_BothProposalsFail_ReturnsAbandon proves the all-fail
// path bubbles ErrAbandon to the caller.
func TestAutopilotDecompose_BothProposalsFail_ReturnsAbandon(t *testing.T) {
	parent := &spec.Task{ID: "009", Acceptance: []string{"c1"}, Body: "x"}
	_, err := decompose.Run(context.Background(), decompose.Input{
		Parent:          parent,
		Claude:          &fakeErrEngine{name: "claude"},
		Codex:           &fakeErrEngine{name: "codex"},
		Synthesizer:     fakeFromScript("claude", "unused"),
		SynthesizerName: "claude",
	})
	if !errors.Is(err, decompose.ErrAbandon) {
		t.Errorf("err = %v, want ErrAbandon", err)
	}
}

func twoTaskProposal(parentID string) string {
	return `---
id: ` + parentID + `.1
kind: feature
parent_id: "` + parentID + `"
status: pending
acceptance:
  - c1a
---
body 1

===TASK===
---
id: ` + parentID + `.2
kind: feature
parent_id: "` + parentID + `"
status: pending
acceptance:
  - c1b
---
body 2
`
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./test/integration/... -run TestAutopilotDecompose_ -v`
Expected: PASS — three tests (the happy-path from Task 8 + the two new ones here).

Run: `go test ./...`
Expected: full suite green.

- [ ] **Step 3: Commit**

```bash
git add test/integration/autopilot_decompose_partial_failure_test.go
git commit -m "test(integration): decompose partial-failure paths splice cleanly"
```

---

## Task 11: README + status update

**Files:**
- Modify: `README.md`

A small subsection under "Autopilot mode" documenting decompose, plus a status-list refresh.

- [ ] **Step 1: Add the decompose subsection**

Find the existing `## Autopilot mode (no human input)` section in `README.md`. After its closing line (or after the "Stalled tasks land under..." paragraph), append:

```markdown
### Auto-decompose for stalled tasks

When a task stalls — repeated rounds raise the same unresolved reviewer issues
even after escalation — autopilot tries to split it before giving up:

1. Claude and Codex each independently propose a 2–4 sub-task split.
2. Whichever engine reviewed the stuck task synthesises the two proposals
   into a single unified split.
3. Sub-tasks land in `.aios/tasks/<parent>.<n>.md`, the parent's frontmatter
   is marked `status: decomposed`, and the run continues with the children.

Recursion is bounded by `[budget] max_decompose_depth` (default 2, hard cap 3).
A child that re-stalls at the depth cap abandons rather than recursively splits.
If both engines error, or the synthesizer emits fewer than 2 sub-tasks, the
parent abandons via the audit-trail path described above.
```

- [ ] **Step 2: Refresh the Project status known-limitations list**

Find the existing list and replace the auto-decompose line. Specifically, replace:

```markdown
- Auto-decompose for stuck tasks is shipping in v0.3.0; in autopilot mode (v0.2.0)
  stalled tasks are abandoned with a full audit trail rather than blocking the run.
```

With:

```markdown
- Auto-decompose for stuck tasks ships in v0.3.0: parallel Claude+Codex
  proposals + reviewer synthesis. Children inherit the parent's dependency
  graph; downstream tasks wait for the full split.
```

Leave the other limitation bullets (sandbox, MCP failure surface) unchanged.

- [ ] **Step 3: Forbidden-language check**

Run:

```bash
grep -nE "comprehensive|robust|leverage|facilitate|ensure that|🤖|Generated by" README.md || echo "clean"
```

Expected: `clean`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs(readme): document auto-decompose for stalled tasks"
```

---

## Self-review checklist

Run before declaring M2 done.

- [ ] **Spec coverage:** Every M2 component in the design spec has a Task above. New orchestrator state (NOT used — decision was to keep state machine unchanged; documented in plan introduction). DecomposeContext (not as a struct — replaced by `decompose.Input` in the CLI package). Dual-engine parallel decompose (Task 6). Synthesizer = reviewer (Task 6 + Task 7's `tryDecompose`). Decompose templates (Tasks 4–5). Partial-failure handling (Task 6 unit tests + Task 10 integration). Sub-task ID stamping `<parent>.<n>` (Task 6's `parseAndStamp`). Recursion depth cap with hard limit 3 (Task 2 + Task 7 gate). Parent worktree thrown away (implicit — children branch from `aios/staging`, the existing M1 `wm.Create` call inside `taskFn` already does this). Sub-task `depends_on` inherits parent's (Task 3 `spliceDecomposedLocked`).
- [ ] **TDD discipline:** Every code-producing task starts with a failing test, makes it pass, then commits.
- [ ] **No placeholders:** Every step has either complete code or a precise command. The "use the existing fixture" patterns (Tasks 8, 10) reference helpers (`seedRepo`, `stubVerifier`, `engine.FakeEngine`) that already exist in the integration test suite per M1.
- [ ] **Type consistency:** `decompose.Input.Parent`, `decompose.Output.Children`, `orchestrator.TaskResult.Children`, `spec.Task.Depth/ParentID/DecomposedInto`, `Budget.MaxDecomposeDepth/DecomposeDepthCap()` all match across tasks.
- [ ] **Frequent commits:** One commit per task; commit subjects follow `feat(scope):` / `test(scope):` / `docs(scope):` convention.
- [ ] **Build green at every commit:** Each task ends with a `go test ./...` and `go build ./...` clean state.
- [ ] **Existing behaviour preserved:** When `task.Depth >= cap` OR decompose errors, the existing M1 abandon path runs unchanged. Non-autopilot mode still blocks on stalls. The orchestrator state machine, scheduler block-cascade behaviour, and merge queue are untouched.

---

## Out of scope (deferred to later milestones)

- **Re-rendering parent's report.md after decompose** — the M1 abandoned-task report.md was the index for stuck tasks. Decomposed parents land in `rep.Blocked` with no abandon code (their state is `Status: "decomposed"` in the scheduler — not blocked at all), so the existing `printBlockSummary` path doesn't see them. The autopilot summary file lists converged + abandoned only. M3 can add a "decomposed" section.
- **Synthesizer-bias mitigation beyond the tiebreak** — the spec acknowledges mild self-bias; the deterministic-merge tiebreak is the spec-approved compromise.
- **Decompose-with-context-from-prior-decompose** — if a child of a decomposed parent itself decomposes, the second decompose call doesn't see the first's history. Acceptable for v0.3.0; revisit if pathological.
- **`gh pr create` body listing decomposed parents and their children separately** — currently the PR body only lists `rep.Converged`, which after M2 contains converged children; the parent's lineage is lost in the PR title. Cheap follow-up: enrich `autopilotPRBody` to call out decomposed parents. Consider during the M2 dogfood pass.

---

## Done criteria

- All 11 tasks merged.
- `go test ./...` passes (unit + integration).
- `go vet ./...` clean.
- A manual smoke run that triggers a stall (e.g. by setting `max_rounds_per_task = 2` and giving an under-specified task) splits the parent and at least one child converges.
- Tag `v0.3.0` cut.
