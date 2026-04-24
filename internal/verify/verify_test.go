package verify

import (
	"context"
	"testing"
	"time"
)

func TestRun_AllPass(t *testing.T) {
	checks := []Check{
		{Name: "echo-ok", Cmd: "echo hi"},
	}
	results := Run(context.Background(), ".", checks, 10*time.Second)
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Status != StatusPassed {
		t.Errorf("status = %s, stderr=%q", results[0].Status, results[0].Stderr)
	}
}

func TestRun_Failure(t *testing.T) {
	checks := []Check{{Name: "bad", Cmd: "sh -c 'exit 3'"}}
	results := Run(context.Background(), ".", checks, 10*time.Second)
	if results[0].Status != StatusFailed {
		t.Errorf("status = %s", results[0].Status)
	}
	if results[0].ExitCode != 3 {
		t.Errorf("exit = %d", results[0].ExitCode)
	}
}

func TestRun_Skipped(t *testing.T) {
	checks := []Check{{Name: "skipped", Cmd: "", Skipped: true}}
	results := Run(context.Background(), ".", checks, 10*time.Second)
	if results[0].Status != StatusSkipped {
		t.Errorf("status = %s", results[0].Status)
	}
}

func TestRun_Empty(t *testing.T) {
	checks := []Check{{Name: "no-cmd", Cmd: ""}}
	results := Run(context.Background(), ".", checks, 10*time.Second)
	if results[0].Status != StatusNotConfigured {
		t.Errorf("status = %s", results[0].Status)
	}
}

func TestAllGreen(t *testing.T) {
	green := []CheckResult{
		{Status: StatusPassed},
		{Status: StatusSkipped},
		{Status: StatusNotConfigured},
	}
	if !AllGreen(green) {
		t.Error("green should be green")
	}
	red := append([]CheckResult{}, green...)
	red = append(red, CheckResult{Status: StatusFailed})
	if AllGreen(red) {
		t.Error("red should not be green")
	}
}
