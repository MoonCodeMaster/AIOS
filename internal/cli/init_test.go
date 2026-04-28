package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_SuccessMessageHintsNext(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// init reads from stdin for verify_*; feed empties.
	root.SetIn(strings.NewReader("\n\n\n\n"))
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	for _, want := range []string{
		"wrote .aios/config.toml",
		"Next:",
		"aios doctor",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("init output missing %q\n--- output ---\n%s", want, buf.String())
		}
	}
}
