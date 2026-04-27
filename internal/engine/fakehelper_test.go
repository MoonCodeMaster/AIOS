package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeHelperSrc is a small Go program that simulates a CLI engine binary.
// It reads AIOS_FAKE_COUNTER (file path), AIOS_FAKE_FAIL_TIMES (int),
// AIOS_FAKE_STDERR (string), and AIOS_FAKE_STDOUT (string) from env.
//
// On each invocation it increments the counter file. If the counter is
// <= AIOS_FAKE_FAIL_TIMES, it writes AIOS_FAKE_STDERR to stderr and
// exits 1 with empty stdout. Otherwise it writes AIOS_FAKE_STDOUT to
// stdout and exits 0.
const fakeHelperSrc = `package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	counterPath := os.Getenv("AIOS_FAKE_COUNTER")
	failTimes, _ := strconv.Atoi(os.Getenv("AIOS_FAKE_FAIL_TIMES"))
	stderr := os.Getenv("AIOS_FAKE_STDERR")
	stdout := os.Getenv("AIOS_FAKE_STDOUT")

	// Increment counter file atomically enough for serial test use.
	count := 0
	if raw, err := os.ReadFile(counterPath); err == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	count++
	_ = os.WriteFile(counterPath, []byte(strconv.Itoa(count)), 0o644)

	if count <= failTimes {
		fmt.Fprint(os.Stderr, stderr)
		os.Exit(1)
	}
	fmt.Fprint(os.Stdout, stdout)
}
`

var (
	fakeHelperOnce sync.Once
	fakeHelperPath string
	fakeHelperErr  error
)

// buildFakeHelper compiles the fake engine binary once per test run and
// returns its path. The binary is placed in a temp dir that persists for
// the lifetime of the test process.
func buildFakeHelper(t *testing.T) string {
	t.Helper()
	fakeHelperOnce.Do(func() {
		dir, err := os.MkdirTemp("", "aios-fake-helper-*")
		if err != nil {
			fakeHelperErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte(fakeHelperSrc), 0o644); err != nil {
			fakeHelperErr = err
			return
		}
		bin := filepath.Join(dir, "fake-engine")
		cmd := exec.Command("go", "build", "-o", bin, src)
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeHelperErr = fmt.Errorf("build fake helper: %w\n%s", err, out)
			return
		}
		fakeHelperPath = bin
	})
	if fakeHelperErr != nil {
		t.Fatalf("buildFakeHelper: %v", fakeHelperErr)
	}
	return fakeHelperPath
}

// readCounter reads the call count from the counter file written by the
// fake helper binary.
func readCounter(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readCounter: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("readCounter parse: %v", err)
	}
	return n
}
