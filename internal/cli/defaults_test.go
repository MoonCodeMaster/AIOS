package cli

import (
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/config"
)

func TestDefaultFlips_V020(t *testing.T) {
	// Simulate loading a minimal config with no explicit values for the
	// flipped fields. applyDefaults should set them to true.
	cfg := &config.Config{SchemaVersion: 1}
	// applyDefaults is called by Load, but we can test the accessor methods
	// on a zero-value struct since they resolve nil → default.
	if !cfg.Budget.HistoryCompression() {
		t.Error("compress_history should default to true in v0.2.0")
	}
	if !cfg.Budget.RespecEnabled() {
		t.Error("respec_on_abandon should default to true in v0.2.0")
	}
	if !cfg.Specgen.CritiqueOn() {
		t.Error("critique_enabled should default to true")
	}
}
