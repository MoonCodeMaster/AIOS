package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBareAIOS_AnyDir_AutoCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)

	// Call gateAIOS directly to verify auto-creation without launching the TUI.
	ctx, err := gateAIOS(context.Background(), "")
	if err != nil {
		t.Fatalf("gateAIOS should work in any directory; got err: %v", err)
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
