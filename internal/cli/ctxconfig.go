package cli

import (
	"context"
	"errors"

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

// RequireConfigFromContext returns the config stashed by gateAIOS, or an error
// if the gate did not run (programmer error — every command above gateAIOS
// level should annotate gateLevelAIOS).
func RequireConfigFromContext(ctx context.Context) (*config.Config, error) {
	cfg, ok := ConfigFromContext(ctx)
	if !ok {
		return nil, errors.New("internal: config not loaded — gate did not run")
	}
	return cfg, nil
}
