package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newExecCmd() *cobra.Command {
	var jsonOutput bool
	c := &cobra.Command{
		Use:           "exec <prompt>",
		Aliases:       []string{"e"},
		Short:         "Run AIOS non-interactively (headless)",
		Long:          "Run the full specgen → decompose → execute pipeline non-interactively.\nOutput is printed to stdout. Use --json for machine-readable JSONL events.",
		Annotations:   map[string]string{gateAnnotation: gateLevelAIOS},
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("exec needs a prompt — try `aios exec \"your task\"`")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			if jsonOutput {
				return runExecJSON(cmd, prompt)
			}
			return runExecHuman(cmd, prompt)
		},
	}
	c.Flags().BoolVar(&jsonOutput, "json", false, "emit JSONL events to stdout")
	return c
}

func runExecHuman(cmd *cobra.Command, prompt string) error {
	w := cmd.OutOrStdout()
	printInfo(w, "⚡ Running: %s", cBold.Sprint(prompt))
	result, err := launchShip(cmd.Context(), prompt)
	if err != nil {
		printError(w, "exec failed: %v", err)
		return err
	}
	switch result.Status {
	case ShipAbandoned:
		printWarn(w, "Some tasks blocked — see .aios/runs/ for details")
	default:
		printSuccess(w, "Done.")
	}
	if result.PRURL != "" {
		printInfo(w, "PR: %s", cCyan.Sprint(result.PRURL))
	}
	return nil
}

func runExecJSON(cmd *cobra.Command, prompt string) error {
	emit := func(event string, data any) {
		obj := map[string]any{"event": event, "data": data}
		b, _ := json.Marshal(obj)
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	}
	emit("start", map[string]string{"prompt": prompt})
	result, err := launchShip(cmd.Context(), prompt)
	if err != nil {
		emit("error", map[string]string{"message": err.Error()})
		return err
	}
	var statusStr string
	switch result.Status {
	case ShipMerged:
		statusStr = "merged"
	case ShipPRRed:
		statusStr = "pr_red"
	case ShipAbandoned:
		statusStr = "abandoned"
	default:
		statusStr = "unknown"
	}
	emit("complete", map[string]any{
		"status":  statusStr,
		"pr_url":  result.PRURL,
		"blocked": result.Status == ShipAbandoned,
	})
	return nil
}
