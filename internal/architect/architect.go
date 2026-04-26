// Package architect drives the multi-round mind-map generation pipeline that
// front-ends `aios architect`. It pairs Claude and Codex against the same
// idea, runs cross-critique and refinement, then asks one of them to
// synthesize three distinct finalists for the user to choose between.
//
// The package owns ONLY the pipeline and the Blueprint data model. The CLI
// glue (selection prompt, spec generation, autopilot chaining) lives in
// internal/cli/architect.go.
package architect

// Blueprint is one mind-map proposal. Every field is plain text — the format
// is human-readable on purpose so users can scan three in a terminal.
type Blueprint struct {
	Title    string
	Tagline  string
	Stance   string // free text in early rounds; "conservative|balanced|ambitious" after synthesis
	MindMap  string // raw indented "- root: ..." block
	Sketch   string // architecture sketch
	DataFlow string // numbered steps
	Tradeoff string // pros/cons block
	Roadmap  string // milestones
	Risks    string // risks + mitigations
}

// Required reports whether a Blueprint has the minimum fields the synthesis
// stage and the to-spec stage both depend on. Used by the parser to drop
// malformed blocks rather than fail the whole round.
func (b Blueprint) Valid() bool {
	return b.Title != "" && b.Stance != "" && b.MindMap != ""
}
