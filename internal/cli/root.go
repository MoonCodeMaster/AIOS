package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
			ship, _ := cmd.Flags().GetBool("ship")
			print, _ := cmd.Flags().GetBool("print")
			resumeID, _ := cmd.Flags().GetString("continue")
			if err := validateRootFlags(args, ship, print, resumeID); err != nil {
				return err
			}
			if len(args) == 0 {
				return launchRepl(cmd.Context(), resumeID)
			}
			prompt := strings.Join(args, " ")
			if ship {
				_, err := launchShip(cmd.Context(), prompt)
				return err
			}
			if print {
				return launchPrintMode(cmd.Context(), prompt)
			}
			return launchOneShot(cmd.Context(), prompt)
		},
	}
	root.PersistentFlags().String("config", ".aios/config.toml", "path to AIOS config")
	root.PersistentFlags().String("log-level", "info", "log level: debug|info|warn|error")
	root.PersistentFlags().Bool("dry-run", false, "print actions without calling engines or writing git")
	root.PersistentFlags().Bool("yolo", false, "on full success, merge aios/staging into base branch")
	root.PersistentFlags().String("continue", "", "resume an REPL session (empty = latest, or pass a session ID); not the same as the 'aios resume' subcommand")
	root.Flags().Bool("ship", false, "run the full ship pipeline: specgen + decompose + execute + PR + merge")
	root.Flags().BoolP("print", "p", false, "print the generated spec to stdout (no project.md write, no shipping)")
	root.AddCommand(newStatusCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newDuelCmd())
	root.AddCommand(newCostCmd())
	root.AddCommand(newLessonsCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newMCPCmd())
	return root
}

// validateRootFlags returns an error if the combination of args + flags
// is illegal for the bare `aios` invocation. Extracted from RunE for
// unit-testability.
func validateRootFlags(args []string, ship, print bool, resumeID string) error {
	if len(args) == 0 {
		if ship || print {
			return fmt.Errorf("--ship and -p require a prompt argument")
		}
		return nil
	}
	if ship && print {
		return fmt.Errorf("--ship and -p are mutually exclusive")
	}
	if resumeID != "" {
		return fmt.Errorf("--continue is REPL-only; do not combine with a prompt")
	}
	return nil
}

// launchShip boots real engines for `aios --ship "prompt"`, runs ShipPrompt,
// and returns the structured result.
func launchShip(ctx context.Context, prompt string) (ShipResult, error) {
	wd, err := os.Getwd()
	if err != nil {
		return ShipResult{}, fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return ShipResult{}, fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	fmt.Fprintf(os.Stdout, "shipping %q…\n", prompt)
	return ShipPrompt(ctx, ShipPromptInput{
		Wd:      wd,
		Prompt:  prompt,
		Claude:  &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:   &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		OnStage: func(name string) { fmt.Fprintf(os.Stdout, "  · %s …\n", name) },
	})
}

// launchOneShot boots real engines for `aios "prompt"`, runs runOneShot.
func launchOneShot(ctx context.Context, prompt string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	return runOneShot(ctx, OneShotInput{
		Wd:     wd,
		Prompt: prompt,
		Claude: &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:  &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		Out:    os.Stdout,
	})
}

// launchPrintMode boots real engines for `aios -p "prompt"`, runs runPrintMode.
func launchPrintMode(ctx context.Context, prompt string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return fmt.Errorf("aios needs an initialised repo here — run `aios init` first: %w", err)
	}
	return runPrintMode(ctx, PrintModeInput{
		Wd:     wd,
		Prompt: prompt,
		Claude: &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec},
		Codex:  &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec},
		Out:    os.Stdout,
	})
}

// launchRepl boots a Repl with real engines and stdio, then runs it.
func launchRepl(ctx context.Context, resumeID string) error {
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
		ResumeID:     resumeID,
	}
	return r.Run(ctx)
}
