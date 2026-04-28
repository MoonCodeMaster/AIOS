package cli

import (
	"context"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/config"
)

func TestConfigContext_RoundTrip(t *testing.T) {
	cfg := &config.Config{SchemaVersion: 1}
	ctx := withConfig(context.Background(), cfg)

	got, ok := ConfigFromContext(ctx)
	if !ok {
		t.Fatal("ConfigFromContext returned ok=false; want true")
	}
	if got != cfg {
		t.Fatalf("ConfigFromContext returned different pointer; want %p got %p", cfg, got)
	}
}

func TestConfigContext_Missing(t *testing.T) {
	if _, ok := ConfigFromContext(context.Background()); ok {
		t.Fatal("ConfigFromContext returned ok=true on empty context; want false")
	}
}

func TestRequireConfigFromContext_ReturnsErrWhenMissing(t *testing.T) {
	if _, err := RequireConfigFromContext(context.Background()); err == nil {
		t.Fatal("RequireConfigFromContext should return error on empty context")
	}
}
