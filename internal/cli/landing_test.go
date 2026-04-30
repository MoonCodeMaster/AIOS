package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareAIOS_OutsideGitRepo_Errors(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{})

	err := root.Execute()
	if err == nil {
		t.Fatal("bare aios outside git repo should error")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("expected git-related error, got: %v", err)
	}
}

func TestBareAIOS_InGitRepo_NoConfig_AutoCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustChdir(t, dir)

	// Call gateAIOS directly to verify auto-creation without launching the TUI.
	ctx, err := gateAIOS(context.Background(), "")
	if err != nil {
		t.Fatalf("gateAIOS should auto-create config; got err: %v", err)
	}
	cfg, ok := ConfigFromContext(ctx)
	if !ok || cfg == nil {
		t.Fatal("gateAIOS did not stash config in context")
	}
	// Verify the file was created on disk.
	cfgPath := filepath.Join(dir, ".aios", "config.toml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected auto-created config at %s, got err: %v", cfgPath, err)
	}
}

func TestBareAIOS_WithPromptOutsideRepo_StillErrors(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)

	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"do something"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for `aios \"prompt\"` outside repo")
	}
}
