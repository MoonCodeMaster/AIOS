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
	// Plan spec: Pipeline section must list registered commands. Both `run`
	// and `ship` are real subcommands as of Task 8.
	if !strings.Contains(out, "Iterate over pending tasks") {
		t.Errorf("Pipeline section missing `run`'s short description")
	}
	if !strings.Contains(out, "Full pipeline: spec") {
		t.Errorf("Pipeline section missing `ship`'s short description")
	}
	if !strings.Contains(out, "unblock") {
		t.Errorf("Inspection section missing `unblock` entry")
	}
}
