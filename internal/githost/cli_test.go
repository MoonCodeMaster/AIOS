package githost

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// fakeExec returns a command builder that produces a process which
// emits stdout/exitcode controlled by the test. Pattern: a tiny helper
// process invoked via os.Args[0] -test.run=TestHelperProcess so we don't
// shell out to anything real. Same approach as os/exec stdlib tests.
func fakeExec(stdout string, exitCode int) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT=" + stdout,
			"HELPER_EXIT=" + map[bool]string{true: "0", false: "1"}[exitCode == 0],
		}
		return cmd
	}
}

func TestCLIHost_OpenPR_HappyPath(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec(`{"number":42,"url":"https://github.com/owner/repo/pull/42"}`, 0),
	}
	pr, err := host.OpenPR(context.Background(), "main", "aios/staging", "title", "body")
	if err != nil {
		t.Fatalf("OpenPR returned error: %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("PR.Number = %d, want 42", pr.Number)
	}
	if !strings.Contains(pr.URL, "/pull/42") {
		t.Errorf("PR.URL = %q, want path /pull/42", pr.URL)
	}
	if pr.Head != "aios/staging" {
		t.Errorf("PR.Head = %q, want aios/staging", pr.Head)
	}
	if pr.Base != "main" {
		t.Errorf("PR.Base = %q, want main", pr.Base)
	}
}

func TestCLIHost_OpenPR_GhFailure(t *testing.T) {
	host := &CLIHost{
		exec: fakeExec("", 1),
	}
	_, err := host.OpenPR(context.Background(), "main", "aios/staging", "title", "body")
	if err == nil {
		t.Fatal("OpenPR should fail when gh exits non-zero")
	}
	if !strings.Contains(err.Error(), "gh pr create") {
		t.Errorf("error %q should reference 'gh pr create'", err.Error())
	}
	_ = errors.New // keep import
}

// TestHelperProcess is the child process spawned by fakeExec. It writes
// HELPER_STDOUT and exits with HELPER_EXIT. Pattern borrowed from os/exec
// stdlib tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if s := os.Getenv("HELPER_STDOUT"); s != "" {
		_, _ = os.Stdout.WriteString(s)
	}
	if os.Getenv("HELPER_EXIT") != "0" {
		os.Exit(1)
	}
	os.Exit(0)
}
