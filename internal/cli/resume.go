package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

func newResumeCmd() *cobra.Command {
	var last bool
	c := &cobra.Command{
		Use:           "resume [session-id]",
		Short:         "Resume a previous REPL session",
		Long:          "Resume a previous interactive session. Without arguments, lists available sessions.\nUse --last to continue the most recent session without a picker.",
		Annotations:   map[string]string{gateAnnotation: gateLevelAIOS},
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var sessionID string
			if len(args) > 0 {
				sessionID = args[0]
			} else if last {
				sessionID = "" // empty = latest
			} else {
				return listSessions(cmd)
			}
			return launchRepl(cmd.Context(), sessionID)
		},
	}
	c.Flags().BoolVar(&last, "last", false, "continue the most recent session without showing a picker")
	return c
}

func listSessions(cmd *cobra.Command) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	sessionsDir := filepath.Join(wd, ".aios", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		printWarn(cmd.OutOrStdout(), "No sessions found. Start one with `aios`.")
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		printWarn(cmd.OutOrStdout(), "No sessions found. Start one with `aios`.")
		return nil
	}
	sort.Strings(ids)
	fmt.Fprintln(cmd.OutOrStdout())
	cBold.Fprintln(cmd.OutOrStdout(), "  Sessions:")
	for i := len(ids) - 1; i >= 0; i-- {
		s, err := LoadSession(filepath.Join(sessionsDir, ids[i]))
		if err != nil {
			continue
		}
		turns := len(s.Turns)
		label := cDim.Sprintf("(%d turns)", turns)
		if i == len(ids)-1 {
			label = cDim.Sprintf("(%d turns)", turns) + cGreen.Sprint(" ← latest")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "    %s  %s\n", cCyan.Sprint(ids[i]), label)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	cDim.Fprintln(cmd.OutOrStdout(), "  Run `aios resume <session-id>` or `aios resume --last`")
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
