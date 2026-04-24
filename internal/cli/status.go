package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Solaxis/aios/internal/spec"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print current task list with status",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			tasksDir := filepath.Join(wd, ".aios", "tasks")
			if _, err := os.Stat(tasksDir); err != nil {
				return fmt.Errorf("no .aios/tasks directory (did you run `aios new`?)")
			}
			tasks, err := spec.LoadTasks(tasksDir)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				fmt.Printf("%s  %-12s  %s\n", t.ID, t.Status, t.Kind)
			}
			return nil
		},
	}
}
