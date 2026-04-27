//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2E_Greenfield(t *testing.T) {
	if os.Getenv("AIOS_E2E") != "1" {
		t.Skip("set AIOS_E2E=1 to run end-to-end tests")
	}
	aios := os.Getenv("AIOS_BIN")
	if aios == "" {
		t.Fatal("AIOS_BIN must point to built aios binary")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(dir+"/README.md", []byte("init\n"), 0o644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	cmd := exec.Command(aios, "init")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("\n\n\n\n\n") // accept all autodetected defaults
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("aios init failed: %v\n%s", err, out)
	}

	cmd = exec.Command(aios, "Build a CLI that reverses its argv, with unit tests")
	cmd.Dir = dir
	// no stdin needed — one-shot spec mode does not prompt
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("aios spec failed: %v\n%s", err, out)
	}

	cmd = exec.Command(aios, "run")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("aios run failed: %v\n%s", err, out)
	}
	// Assert staging has at least one commit beyond the initial.
	out, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "aios/staging").Output()
	if len(out) < 1 || out[0] <= '1' {
		t.Errorf("expected aios/staging to have >1 commits, got %q", out)
	}
}
