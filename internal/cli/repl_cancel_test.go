package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
)

// cancelOnInvokeEngine cancels the context the moment it's first called,
// then returns context.Canceled — simulating a SIGINT that arrives mid-pipeline.
type cancelOnInvokeEngine struct {
	name   string
	cancel context.CancelFunc
}

func (e *cancelOnInvokeEngine) Name() string { return e.name }
func (e *cancelOnInvokeEngine) Invoke(ctx context.Context, _ engine.InvokeRequest) (*engine.InvokeResponse, error) {
	e.cancel()
	return nil, context.Canceled
}

// TestReplRefusesWhenCLIMissing_Cancel verifies the pre-TUI gate still works.
func TestReplRefusesWhenCLIMissing_Cancel(t *testing.T) {
	wd := t.TempDir()
	r := &Repl{
		Wd:           wd,
		In:           strings.NewReader(""),
		Out:          &bytes.Buffer{},
		ClaudeBinary: "this-binary-does-not-exist-aios-test",
		CodexBinary:  "codex",
		LookPath:     exec.LookPath,
		Claude:       &cancelOnInvokeEngine{name: "claude"},
		Codex:        &cancelOnInvokeEngine{name: "codex"},
	}
	err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("Run should have returned an error when claude binary is missing")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("error should mention missing claude; got: %v", err)
	}
}

// TestReplBootSessionWithCancel verifies bootSession works even with a cancelled context.
func TestReplBootSessionWithCancel(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		Claude: &engine.FakeEngine{Name_: "claude"},
		Codex:  &engine.FakeEngine{Name_: "codex"},
	}
	if err := r.bootSession(); err != nil {
		t.Fatalf("bootSession: %v", err)
	}
	if r.session == nil || r.session.ID == "" {
		t.Fatal("session not created")
	}
}
