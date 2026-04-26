package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/spf13/cobra"
)

// Version is stamped by GoReleaser at build time.
var Version = "dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "aios",
		Short:   "AIOS — dual-AI project orchestrator",
		Long:    "Drives Claude CLI and Codex CLI as a coder↔reviewer pair over a spec-driven task queue.",
		Version: Version,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare `aios` (no subcommand, no positional args) launches the REPL.
			if len(args) > 0 {
				return cmd.Help()
			}
			return launchRepl(cmd.Context())
		},
	}
	root.PersistentFlags().String("config", ".aios/config.toml", "path to AIOS config")
	root.PersistentFlags().String("log-level", "info", "log level: debug|info|warn|error")
	root.PersistentFlags().Bool("dry-run", false, "print actions without calling engines or writing git")
	root.PersistentFlags().Bool("yolo", false, "on full success, merge aios/staging into base branch")
	root.PersistentFlags().String("resume", "", "resume an REPL session (empty = latest, or pass a session ID)")
	root.AddCommand(newStatusCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newNewCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newAutopilotCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newArchitectCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newDuelCmd())
	root.AddCommand(newCostCmd())
	root.AddCommand(newLessonsCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newMCPCmd())
	return root
}

// launchRepl boots a Repl with real engines and stdio, then runs it.
func launchRepl(ctx context.Context) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	r := &Repl{
		Wd:           wd,
		In:           os.Stdin,
		Out:          os.Stdout,
		Claude:       &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:        &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		ClaudeBinary: cfg.Engines.Claude.Binary,
		CodexBinary:  cfg.Engines.Codex.Binary,
	}
	return r.Run(ctx)
}
