package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

func newShipCmd() *cobra.Command {
	c := &cobra.Command{
		Use:           "ship <prompt>",
		Short:         "Full pipeline: spec → tasks → PR → merge",
		Annotations:   map[string]string{gateAnnotation: gateLevelAIOS},
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("ship needs a prompt — try `aios ship \"your idea\"`")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			_, err := launchShip(cmd.Context(), prompt)
			return err
		},
	}
	c.Flags().Bool("dry-run", false, "print actions without calling engines or writing git")
	c.Flags().Bool("yolo", false, "on full success, merge aios/staging into base branch")
	return c
}
