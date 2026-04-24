//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2E_Bugfix(t *testing.T) {
	if os.Getenv("AIOS_E2E") != "1" {
		t.Skip()
	}
	aios := os.Getenv("AIOS_BIN")
	if aios == "" {
		t.Fatal("AIOS_BIN required")
	}
	dir := t.TempDir()
	// Seed: a Go project with one failing test.
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		_ = exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
	}
	_ = os.WriteFile(dir+"/go.mod", []byte("module ex\n\ngo 1.22\n"), 0o644)
	_ = os.WriteFile(dir+"/add.go", []byte("package ex\nfunc Add(a,b int) int { return a-b }\n"), 0o644)
	_ = os.WriteFile(dir+"/add_test.go", []byte("package ex\nimport \"testing\"\nfunc TestAdd(t *testing.T){ if Add(2,3)!=5 { t.Fail() }}\n"), 0o644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	initCmd := exec.Command(aios, "init")
	initCmd.Dir = dir
	_ = initCmd.Run()
	cmd := exec.Command(aios, "new", "Make the failing test in add_test.go pass by fixing add.go. Do not change add_test.go.")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("y\n")
	_ = cmd.Run()
	cmd = exec.Command(aios, "run")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Confirm tests pass on staging.
	_ = exec.Command("git", "-C", dir, "checkout", "aios/staging").Run()
	if out, err := exec.Command("go", "test", "./...").Output(); err != nil {
		t.Fatalf("tests still failing on staging: %v\n%s", err, out)
	}
}
