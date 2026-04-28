package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MoonCodeMaster/AIOS/internal/config"
)

// Gate annotation key set on every cobra.Command via cmd.Annotations.
// Values: "none", "git", "aios". Empty defaults to "aios".
const gateAnnotation = "aios.gate"

const (
	gateLevelNone = "none"
	gateLevelGit  = "git"
	gateLevelAIOS = "aios"
)

// gateFunc runs a gate check against cwd. configPath is honored only by gateAIOS;
// pass "" to use the default `<cwd>/.aios/config.toml`.
type gateFunc func(ctx context.Context, configPath string) (context.Context, error)

func selectGate(level string) gateFunc {
	switch level {
	case gateLevelNone:
		return gateNone
	case gateLevelGit:
		return gateGit
	case gateLevelAIOS, "":
		return gateAIOS
	default:
		// Defensive: unknown values fail closed (toughest gate).
		return gateAIOS
	}
}

func gateNone(ctx context.Context, _ string) (context.Context, error) {
	return ctx, nil
}

func gateGit(ctx context.Context, _ string) (context.Context, error) {
	wd, err := os.Getwd()
	if err != nil {
		return ctx, fmt.Errorf("cannot determine working directory: %w", err)
	}
	if _, err := os.Stat(filepath.Join(wd, ".git")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ctx, errors.New("not a git repo — run `git init` first")
		}
		return ctx, fmt.Errorf("stat .git: %w", err)
	}
	return ctx, nil
}

func gateAIOS(ctx context.Context, configPath string) (context.Context, error) {
	// Layered: must be in a git repo first.
	ctx, err := gateGit(ctx, "")
	if err != nil {
		return ctx, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return ctx, fmt.Errorf("cannot determine working directory: %w", err)
	}
	path := configPath
	if path == "" {
		path = filepath.Join(wd, ".aios", "config.toml")
	}
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ctx, errors.New("not an AIOS repo — run `aios init` here, or cd to an existing one")
		}
		return ctx, fmt.Errorf("load %s: %w", path, err)
	}
	return withConfig(ctx, cfg), nil
}
