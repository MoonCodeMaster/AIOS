package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MoonCodeMaster/AIOS/internal/spec"
	"github.com/spf13/cobra"
)

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <task-id>",
		Short: "Unblock a blocked task with an optional note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			path := filepath.Join(wd, ".aios", "tasks", taskID+".md")
			raw, err := os.ReadFile(path)
			if err != nil {
				// allow users to pass a filename with slug
				matches, _ := filepath.Glob(filepath.Join(wd, ".aios", "tasks", taskID+"*.md"))
				if len(matches) != 1 {
					return fmt.Errorf("task %s not found", taskID)
				}
				path = matches[0]
				raw, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			task, err := spec.ParseTask(string(raw))
			if err != nil {
				return err
			}
			if task.Status != "blocked" {
				return fmt.Errorf("task %s is %s, not blocked", task.ID, task.Status)
			}
			fmt.Print("Note to send to the next coder prompt (empty to skip): ")
			r := bufio.NewReader(os.Stdin)
			note, err := r.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("read note: %w", err)
			}
			note = strings.TrimRight(note, "\n")

			updated := flipStatusToPending(string(raw), note)
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return err
			}
			fmt.Printf("Task %s marked pending. Run `aios run` to resume.\n", task.ID)
			return nil
		},
	}
}

// flipStatusToPending edits the frontmatter's status line and appends a
// resume note as a body trailer. Conservative string ops: avoids rewriting
// the entire YAML tree.
func flipStatusToPending(src, note string) string {
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "status:") {
			indent := ln[:len(ln)-len(strings.TrimLeft(ln, " "))]
			lines[i] = indent + "status: pending"
			break
		}
	}
	out := strings.Join(lines, "\n")
	if note != "" {
		out += "\n\n> Resume note: " + note + "\n"
	}
	return out
}

var _ = errors.New
