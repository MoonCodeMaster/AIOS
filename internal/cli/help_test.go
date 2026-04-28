package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelp_GroupedSections(t *testing.T) {
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, header := range []string{"Session:", "Pipeline:", "Setup:", "Inspection:", "Flags:"} {
		if !strings.Contains(out, header) {
			t.Errorf("help output missing section %q\n--- output ---\n%s", header, out)
		}
	}
	// Plan spec: Pipeline section must list registered commands. `run` exists
	// today; `ship` will be added in Task 8 (assertion deliberately omitted).
	if !strings.Contains(out, "Iterate over pending tasks") {
		t.Errorf("Pipeline section missing `run`'s short description")
	}
	// Inspection currently lists `resume`; Task 11 renames to `unblock`.
	if !strings.Contains(out, "resume") && !strings.Contains(out, "unblock") {
		t.Errorf("Inspection section missing resume/unblock entry")
	}
}
