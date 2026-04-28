package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/spf13/cobra"
)

// Version is stamped by GoReleaser at build time.
var Version = "dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "aios",
		Short:         "AIOS — dual-AI project orchestrator",
		Long:          "Drives Claude CLI and Codex CLI as a coder↔reviewer pair over a spec-driven task queue.",
		Version:       Version,
		Args:          cobra.ArbitraryArgs,
		Annotations:   map[string]string{gateAnnotation: gateLevelAIOS},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Special case: bare `aios` (root command, no positional args, no
			// pipeline-mode flags) prints a landing card instead of erroring
			// when .aios/config.toml is missing.
			if cmd == cmd.Root() {
				print, _ := cmd.Flags().GetBool("print")
				resumeID, _ := cmd.Flags().GetString("continue")
				configChanged := cmd.Flags().Changed("config")
				if len(args) == 0 && !print && resumeID == "" && !configChanged && !hasAIOSConfig() {
					printLandingCard(cmd.OutOrStdout())
					// Mark RunE as handled so the original RunE (which would
					// launch REPL) is bypassed.
					cmd.RunE = func(*cobra.Command, []string) error { return nil }
					return nil
				}
			}
			// Help, completion script generator, and shell-completion backends
			// (__complete / __completeNoDesc — invoked by generated bash/zsh/fish
			// scripts on every tab press) all bypass gating.
			if cmd.Name() == "help" || cmd.CalledAs() == "help" ||
				cmd.Name() == "completion" ||
				cmd.Name() == cobra.ShellCompRequestCmd ||
				cmd.Name() == cobra.ShellCompNoDescRequestCmd ||
				(cmd.Parent() != nil && cmd.Parent().Name() == "completion") {
				return nil
			}
			level := cmd.Annotations[gateAnnotation]
			gate := selectGate(level)
			configPath, _ := cmd.Flags().GetString("config")
			ctx, err := gate(cmd.Context(), configPath)
			if err != nil {
				return err
			}
			cmd.SetContext(ctx)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			print, _ := cmd.Flags().GetBool("print")
			resumeID, _ := cmd.Flags().GetString("continue")
			if err := validateRootFlags(args, print, resumeID); err != nil {
				return err
			}
			if len(args) == 0 {
				return launchRepl(cmd.Context(), resumeID)
			}
			prompt := strings.Join(args, " ")
			if print {
				return launchPrintMode(cmd.Context(), prompt)
			}
			return launchOneShot(cmd.Context(), prompt)
		},
	}
	root.PersistentFlags().String("config", ".aios/config.toml", "path to AIOS config")
	root.PersistentFlags().String("log-level", "info", "log level: debug|info|warn|error")
	root.Flags().StringP("continue", "c", "", "resume an REPL session (empty = latest, or pass a session ID)")
	root.Flags().BoolP("print", "p", false, "print the generated spec to stdout (no project.md write, no shipping)")
	root.AddCommand(newShipCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newUnblockCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newDuelCmd())
	root.AddCommand(newCostCmd())
	root.AddCommand(newLessonsCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newMCPCmd())
	installRootHelp(root)
	return root
}

// validateRootFlags returns an error if the combination of args + flags
// is illegal for the bare `aios` invocation. Extracted from RunE for
// unit-testability.
func validateRootFlags(args []string, print bool, resumeID string) error {
	if len(args) == 0 {
		if print {
			return errors.New("-p requires a prompt argument")
		}
		return nil
	}
	if resumeID != "" {
		return errors.New("--continue is REPL-only; do not combine with a prompt")
	}
	return nil
}

// launchShip boots real engines for `aios ship "prompt"`, runs ShipPrompt,
// and returns the structured result.
func launchShip(ctx context.Context, prompt string) (ShipResult, error) {
	cfg, err := RequireConfigFromContext(ctx)
	if err != nil {
		return ShipResult{}, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return ShipResult{}, fmt.Errorf("getwd: %w", err)
	}
	fmt.Fprintf(os.Stdout, "shipping %q…\n", prompt)
	return ShipPrompt(ctx, ShipPromptInput{
		Wd:                wd,
		Prompt:            prompt,
		Claude:            &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Claude)},
		Codex:             &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Codex)},
		OnStage:           func(name string) { fmt.Fprintf(os.Stdout, "  · %s …\n", name) },
		CritiqueEnabled:   cfg.Specgen.CritiqueOn(),
		CritiqueThreshold: cfg.Specgen.Threshold(),
	})
}

// launchOneShot boots real engines for `aios "prompt"`, runs runOneShot.
func launchOneShot(ctx context.Context, prompt string) error {
	cfg, err := RequireConfigFromContext(ctx)
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	return runOneShot(ctx, OneShotInput{
		Wd:                wd,
		Prompt:            prompt,
		Claude:            &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Claude)},
		Codex:             &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Codex)},
		Out:               os.Stdout,
		CritiqueEnabled:   cfg.Specgen.CritiqueOn(),
		CritiqueThreshold: cfg.Specgen.Threshold(),
	})
}

// launchPrintMode boots real engines for `aios -p "prompt"`, runs runPrintMode.
func launchPrintMode(ctx context.Context, prompt string) error {
	cfg, err := RequireConfigFromContext(ctx)
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	return runPrintMode(ctx, PrintModeInput{
		Wd:                wd,
		Prompt:            prompt,
		Claude:            &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Claude)},
		Codex:             &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Codex)},
		Out:               os.Stdout,
		CritiqueEnabled:   cfg.Specgen.CritiqueOn(),
		CritiqueThreshold: cfg.Specgen.Threshold(),
	})
}

// launchRepl boots a Repl with real engines and stdio, then runs it.
func launchRepl(ctx context.Context, resumeID string) error {
	cfg, err := RequireConfigFromContext(ctx)
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	r := &Repl{
		Wd:                wd,
		In:                os.Stdin,
		Out:               os.Stdout,
		Claude:            &engine.ClaudeEngine{Binary: cfg.Engines.Claude.Binary, ExtraArgs: cfg.Engines.Claude.ExtraArgs, TimeoutSec: cfg.Engines.Claude.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Claude)},
		Codex:             &engine.CodexEngine{Binary: cfg.Engines.Codex.Binary, ExtraArgs: cfg.Engines.Codex.ExtraArgs, TimeoutSec: cfg.Engines.Codex.TimeoutSec, Retry: retryPolicyFrom(cfg.Engines.Codex)},
		ClaudeBinary:      cfg.Engines.Claude.Binary,
		CodexBinary:       cfg.Engines.Codex.Binary,
		ResumeID:          resumeID,
		CritiqueEnabled:   cfg.Specgen.CritiqueOn(),
		CritiqueThreshold: cfg.Specgen.Threshold(),
	}
	return r.Run(ctx)
}
