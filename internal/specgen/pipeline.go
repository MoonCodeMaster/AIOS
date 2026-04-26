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
