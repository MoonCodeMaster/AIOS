package cli

import (
	"bytes"
	"context"
	"os"
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

// When ctx is cancelled mid-pipeline (the Ctrl+C path), the REPL must exit
// cleanly without printing "turn failed: context canceled" — that line was
// noise the user saw on every Ctrl+C.
func TestReplExitsQuietlyOnCancel(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	r := &Repl{
		Wd:     wd,
		In:     strings.NewReader("build a thing\n"),
		Out:    stdout,
		Claude: &cancelOnInvokeEngine{name: "claude", cancel: cancel},
		Codex:  &cancelOnInvokeEngine{name: "codex", cancel: cancel},
	}
	if err := r.Run(ctx); err != nil {
		t.Fatalf("Run returned %v, want nil (graceful exit on cancel)", err)
	}

	out := stdout.String()
	if strings.Contains(out, "turn failed") {
		t.Errorf("REPL printed 'turn failed' on cancel; should be silent. got: %s", out)
	}
}
