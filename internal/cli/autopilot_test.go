package cli

import (
	"strings"
	"testing"
)

// TestAutopilotCmdHelpMentionsKeyConcepts is a smoke test: the help text should
// describe the contract from `aios autopilot --help` so a user knows what to
// expect before running it.
func TestAutopilotCmdHelpMentionsKeyConcepts(t *testing.T) {
	c := newAutopilotCmd()
	help := c.Long
	for _, want := range []string{"PR", "merge", "gh"} {
		if !strings.Contains(help, want) {
			t.Errorf("autopilot help should mention %q; got: %q", want, help)
		}
	}
}
