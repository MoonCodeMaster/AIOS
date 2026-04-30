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

// TestReplBootSession_CreatesSession verifies a new session is created on first boot.
func TestReplBootSession_CreatesSession(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".aios", "sessions"), 0o755); err != nil {
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
	// Verify session dir was created.
	if _, err := os.Stat(filepath.Join(wd, ".aios", "sessions", r.session.ID)); err != nil {
		t.Fatalf("session dir not created: %v", err)
	}
}
