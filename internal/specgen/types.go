package specgen

import (
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
)

// Input is the full set of inputs to one Generate call.
type Input struct {
	UserRequest    string        // current turn's prompt
	PriorTurns     []Turn        // previous {user message, final spec produced}
	CurrentSpec    string        // empty on first turn; existing .aios/project.md otherwise
	ProjectContext string        // optional repo summary; may be empty
	Claude         engine.Engine // required
	Codex          engine.Engine // required
	Recorder       *run.Recorder // optional; nil = do not persist intermediates
	// OnStageStart and OnStageEnd may be invoked concurrently for the
	// draft-claude and draft-codex stages, which run in parallel goroutines.
	OnStageStart func(name string)
	OnStageEnd   func(name string, err error)
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
