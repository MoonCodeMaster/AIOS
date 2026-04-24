package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPreflight_DirtyTree_Refuses(t *testing.T) {
	repo := seedRepo(t)
	// make the tree dirty
	_ = os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644)

	aios := os.Getenv("AIOS_BIN")
	if aios == "" {
		t.Skip("set AIOS_BIN to the built binary to run this test")
	}
	cmd := exec.Command(aios, "run")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected refusal, got success: %s", out)
	}
}
