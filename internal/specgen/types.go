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
	// CritiqueEnabled gates stages 5 (critique) and 6 (conditional refine).
	CritiqueEnabled bool
	// CritiqueThreshold is the minimum score (0-12) to skip the refine stage.
	CritiqueThreshold int
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
	// Score is the parsed critique score. Nil when critique is disabled or
	// the critique stage was skipped due to error.
	Score          *SpecScore
	CritiqueIssues []CritiqueIssue
	Refined        bool
}

// SpecScore holds the four-dimension critique score.
type SpecScore struct {
	Completeness      int  `json:"completeness"`
	Testability       int  `json:"testability"`
	ScopeCoherence    int  `json:"scope_coherence"`
	ConstraintClarity int  `json:"constraint_clarity"`
	Total             int  `json:"total"`
	Pass              bool `json:"pass"`
}

// CritiqueIssue is a single issue raised by the critique stage.
type CritiqueIssue struct {
	Dimension string `json:"dimension"`
	Note      string `json:"note"`
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

// RegenerateInput is the input for the Regenerate pipeline, which takes an
// existing spec and failure feedback to produce a revised spec. It skips the
// dual-draft stages and goes straight to a feedback-aware merge.
type RegenerateInput struct {
	OriginalSpec      string
	Feedback          string
	Claude            engine.Engine
	Codex             engine.Engine
	Recorder          *run.Recorder
	PolishEngine      string // "claude" or "codex" — the engine that polished the original spec
	CritiqueEnabled   bool
	CritiqueThreshold int
	OnStageStart      func(string)
	OnStageEnd        func(string, error)
}
