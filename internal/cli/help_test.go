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
}
