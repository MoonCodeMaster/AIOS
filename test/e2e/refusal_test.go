//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2E_Refusal(t *testing.T) {
	if os.Getenv("AIOS_E2E") != "1" {
		t.Skip()
	}
	aios := os.Getenv("AIOS_BIN")
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		_ = exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
	}
	_ = os.WriteFile(dir+"/README.md", []byte("x"), 0o644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	initCmd := exec.Command(aios, "init")
	initCmd.Dir = dir
	_ = initCmd.Run()
	cmd := exec.Command(aios, "new", "Implement SHA-256 in exactly 3 lines of Go with no external libs and all tests green")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("y\n")
	_ = cmd.Run()
	cmd = exec.Command(aios, "run")
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit (blocked)")
	}
}
