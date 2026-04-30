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
var Version = "0.3.9"

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
			// Apply --no-color if set.
			if nc, _ := cmd.Flags().GetBool("no-color"); nc {
				SetNoColor(true)
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

			// Cobra retains c.ctx across Execute() calls on the same root, so
			// per-execution markers (e.g. landing-card flag) would leak into
			// the next run when a root command is reused (tests, embeddings).
			// Reset to a fresh context here so each Execute() starts clean.
			cmd.Root().SetContext(context.Background())

			// Renamed-command migration hint: fire BEFORE the gate so v0.2 users
			// who run `aios resume task-1` from any directory get the hint instead
			// of a misleading "not a git repo" gate error.
			if cmd == cmd.Root() && len(args) > 0 {
				if hint := renamedCommandHint(args[0]); hint != "" {
					return errors.New(hint)
				}
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

			// Claude-CLI-style space-separated `aios -c <id>`: pflag sees `-c` alone
			// (consuming the NoOptDefVal sentinel) and treats <id> as a positional.
			// Reinterpret: if -c was given (sentinel present), no -p, and exactly one
			// positional, that positional IS the session ID.
			if resumeID == "@latest" && !print && len(args) == 1 {
				return launchRepl(cmd.Context(), args[0])
			}

			if err := validateRootFlags(args, print, resumeID); err != nil {
				return err
			}
			// Renamed-command hint is handled in PersistentPreRunE so it fires
			// before the gate (v0.2 users outside a repo still get the hint).
			if len(args) == 0 {
				printBanner(cmd.OutOrStdout())
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
	root.PersistentFlags().BoolVarP(&Quiet, "quiet", "q", false, "suppress progress output; only errors and final results")
	root.PersistentFlags().BoolVar(&Verbose, "verbose", false, "enable debug-level output")
	root.PersistentFlags().Bool("no-color", false, "disable colored output")
	root.PersistentFlags().Lookup("no-color").NoOptDefVal = "true"
	root.PersistentFlags().StringVarP(&ModelOverride, "model", "m", "", "override coder engine (claude or codex)")
	root.Flags().StringP("continue", "c", "", "resume an REPL session (empty = latest, or pass a session ID)")
	// NoOptDefVal makes -c (and --continue) accept being given without an
	// argument; the sentinel "@latest" is translated by launchRepl into the
	// empty-string semantics that bootSession recognises as "use latest".
	if f := root.Flags().Lookup("continue"); f != nil {
		f.NoOptDefVal = "@latest"
	}
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
	root.AddCommand(newExecCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newResumeCmd())
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
	if resumeID == "@latest" {
		resumeID = "" // bootSession treats empty as "auto-resume latest if any"
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

// renamedCommandHint returns a migration hint when a user types a v0.2
// command name as the first positional arg of bare `aios`. Empty string
// means "no hint, proceed as normal prompt".
func renamedCommandHint(arg string) string {
	switch arg {
	case "ship":
		return ""
	}
	return ""
}
