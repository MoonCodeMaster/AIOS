package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
)

// OneShotInput bundles the inputs to a non-interactive single-prompt
// invocation of `aios "prompt"`. Engines are injectable for tests.
type OneShotInput struct {
	Wd                string
	Prompt            string
	Claude            engine.Engine
	Codex             engine.Engine
	Out               io.Writer
	CritiqueEnabled   bool
	CritiqueThreshold int
}

// runOneShot runs specgen on the prompt, writes the polished spec to
// .aios/project.md, and prints a brief confirmation to Out. Used by
// `aios "prompt"` (no flags). Does NOT ship — that's `aios ship`.
func runOneShot(ctx context.Context, in OneShotInput) error {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(in.Wd, ".aios", "runs"), runID)
	if err != nil {
		return fmt.Errorf("open run dir: %w", err)
	}
	fmt.Fprintln(in.Out, "running 4-stage pipeline…")
	// OnStageStart may fire concurrently for the parallel draft stages,
	// so guard writes to in.Out with a mutex.
	var outMu sync.Mutex
	out, err := specgen.Generate(ctx, specgen.Input{
		UserRequest:       in.Prompt,
		Claude:            in.Claude,
		Codex:             in.Codex,
		Recorder:          rec,
		CritiqueEnabled:   in.CritiqueEnabled,
		CritiqueThreshold: in.CritiqueThreshold,
		OnStageStart: func(name string) {
			outMu.Lock()
			defer outMu.Unlock()
			fmt.Fprintf(in.Out, "  · %s …\n", name)
		},
	})
	if err != nil {
		return fmt.Errorf("specgen: %w", err)
	}
	specPath := filepath.Join(in.Wd, ".aios", "project.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(specPath, []byte(out.Final), 0o644); err != nil {
		return err
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(in.Out, "  ! %s\n", w)
	}
	fmt.Fprintf(in.Out, "Spec written to %s. Run `aios ship %q` to implement, or open the REPL with `aios` to refine.\n", specPath, in.Prompt)
	return nil
}

// PrintModeInput bundles the inputs for `aios -p "prompt"`.
type PrintModeInput struct {
	Wd                string
	Prompt            string
	Claude            engine.Engine
	Codex             engine.Engine
	Out               io.Writer
	CritiqueEnabled   bool
	CritiqueThreshold int
}

// runPrintMode runs specgen and writes ONLY the polished spec to Out.
// No project.md, no progress noise, no run dir summary on stdout.
// Audit artifacts under .aios/runs/<id>/specgen/ still get written
// (the Recorder is bound) so debugging is preserved.
func runPrintMode(ctx context.Context, in PrintModeInput) error {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(in.Wd, ".aios", "runs"), runID)
	if err != nil {
		return fmt.Errorf("open run dir: %w", err)
	}
	out, err := specgen.Generate(ctx, specgen.Input{
		UserRequest:       in.Prompt,
		Claude:            in.Claude,
		Codex:             in.Codex,
		Recorder:          rec,
		CritiqueEnabled:   in.CritiqueEnabled,
		CritiqueThreshold: in.CritiqueThreshold,
	})
	if err != nil {
		return fmt.Errorf("specgen: %w", err)
	}
	_, err = fmt.Fprint(in.Out, out.Final)
	return err
}
