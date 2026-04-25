package cli

import (
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
	}
	root.PersistentFlags().String("config", ".aios/config.toml", "path to AIOS config")
	root.PersistentFlags().String("log-level", "info", "log level: debug|info|warn|error")
	root.PersistentFlags().Bool("dry-run", false, "print actions without calling engines or writing git")
	root.PersistentFlags().Bool("yolo", false, "on full success, merge aios/staging into base branch")
	root.AddCommand(newStatusCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newNewCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newAutopilotCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newDuelCmd())
	root.AddCommand(newCostCmd())
	return root
}
