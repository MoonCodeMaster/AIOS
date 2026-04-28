package cli

import (
	"context"

	"github.com/MoonCodeMaster/AIOS/internal/config"
)

type configCtxKey struct{}

func withConfig(ctx context.Context, cfg *config.Config) context.Context {
	return context.WithValue(ctx, configCtxKey{}, cfg)
}

// ConfigFromContext returns the *config.Config that gateAIOS stashed during
// PersistentPreRunE. Subcommands fetch via this helper instead of re-loading
// .aios/config.toml.
func ConfigFromContext(ctx context.Context) (*config.Config, bool) {
	cfg, ok := ctx.Value(configCtxKey{}).(*config.Config)
	return cfg, ok
}
