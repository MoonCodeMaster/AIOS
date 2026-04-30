package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
	"github.com/MoonCodeMaster/AIOS/internal/tui"
)

// Repl is one interactive AIOS session.
type Repl struct {
	Wd      string
	In      io.Reader
	Out     io.Writer
	Claude  engine.Engine
	Codex   engine.Engine
	NoColor bool

	ClaudeBinary string
	CodexBinary  string
	LookPath     func(string) (string, error) // injectable for tests; defaults to exec.LookPath

	ShipFn func(ctx context.Context, wd string) error // injectable for tests; defaults to runAutopilotShip

	ResumeID string // empty = use LatestSession; specific ID = LoadSession(<id>)

	CritiqueEnabled   bool
	CritiqueThreshold int

	session *Session
	ctx     context.Context
	prog    *tea.Program
}

// Run launches the full-screen bubbletea TUI and runs the REPL loop.
func (r *Repl) Run(ctx context.Context) error {
	if r.LookPath == nil {
		r.LookPath = exec.LookPath
	}
	if r.ResumeID != "" {
		if err := r.bootSession(); err != nil {
			return err
		}
	}
	// Defer binary checks to first engine use — don't block startup.
	// The TUI will show a clear error if an engine is missing when invoked.
	if err := r.bootSession(); err != nil {
		return err
	}
	r.ctx = ctx

	app := tui.New(Version, r.session.ID, len(r.session.Turns))
	app.OnSubmit = r.onSubmit
	app.OnShip = r.onShip
	app.OnExit = func() {}

	r.prog = tea.NewProgram(app,
		tea.WithAltScreen(),
	)

	if _, err := r.prog.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// onSubmit handles user input from the TUI.
func (r *Repl) onSubmit(msg string) tea.Cmd {
	switch msg {
	case "/show":
		return r.cmdShowSpec()
	case "/clear":
		r.session.Turns = nil
		_ = r.session.Save()
		return nil
	}
	// Normal message — run specgen in background.
	return r.cmdRunTurn(msg)
}

func (r *Repl) cmdShowSpec() tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(r.session.SpecPath)
		if err != nil {
			return tui.SpecDone("", 0, []string{"No spec yet."}, nil)
		}
		return tui.SpecDone(string(data), strings.Count(string(data), "\n")+1, nil, nil)
	}
}

func (r *Repl) cmdRunTurn(msg string) tea.Cmd {
	return func() tea.Msg {
		runID := time.Now().UTC().Format("2006-01-02T15-04-05")
		rec, err := run.Open(filepath.Join(r.Wd, ".aios", "runs"), runID)
		if err != nil {
			return tui.SpecDone("", 0, nil, err)
		}
		currentSpec := ""
		if data, err := os.ReadFile(r.session.SpecPath); err == nil {
			currentSpec = string(data)
		}
		prior := make([]specgen.Turn, len(r.session.Turns))
		for i, t := range r.session.Turns {
			prior[i] = specgen.Turn{UserMessage: t.UserMessage, FinalSpec: t.SpecAfter}
		}

		prog := r.prog
		in := specgen.Input{
			UserRequest:       msg,
			PriorTurns:        prior,
			CurrentSpec:       currentSpec,
			Claude:            r.Claude,
			Codex:             r.Codex,
			Recorder:          rec,
			CritiqueEnabled:   r.CritiqueEnabled,
			CritiqueThreshold: r.CritiqueThreshold,
			OnStageStart: func(name string) {
				prog.Send(tui.StageStart(name))
			},
			OnStageEnd: func(name string, stageErr error) {
				prog.Send(tui.StageEnd(name, 0, stageErr))
			},
			OnStageProgress: func(name string, elapsed time.Duration) {
				prog.Send(tui.StageEnd(name, elapsed, nil))
			},
		}
		out, genErr := specgen.Generate(r.ctx, in)
		if genErr != nil {
			return tui.SpecDone("", 0, nil, genErr)
		}
		// Persist spec.
		if err := os.MkdirAll(filepath.Dir(r.session.SpecPath), 0o755); err != nil {
			return tui.SpecDone("", 0, nil, err)
		}
		if err := os.WriteFile(r.session.SpecPath, []byte(out.Final), 0o644); err != nil {
			return tui.SpecDone("", 0, nil, err)
		}
		r.session.Turns = append(r.session.Turns, SessionTurn{
			Timestamp: time.Now().UTC(), UserMessage: msg, SpecAfter: out.Final, RunID: runID,
		})
		_ = r.session.Save()

		lineCount := strings.Count(out.Final, "\n") + 1
		return tui.SpecDone(out.Final, lineCount, out.Warnings, nil)
	}
}

func (r *Repl) onShip() tea.Cmd {
	return func() tea.Msg {
		shipFn := r.ShipFn
		if shipFn == nil {
			shipFn = runAutopilotShip
		}
		err := shipFn(r.ctx, r.Wd)
		if err != nil {
			return tui.SpecDone("", 0, nil, fmt.Errorf("ship failed: %w", err))
		}
		return tui.SpecDone("", 0, []string{"Ship complete."}, nil)
	}
}

func (r *Repl) bootSession() error {
	if r.session != nil {
		return nil
	}
	sessionsDir := filepath.Join(r.Wd, ".aios", "sessions")
	switch {
	case r.ResumeID != "":
		s, err := LoadSession(filepath.Join(sessionsDir, r.ResumeID))
		if err != nil {
			return fmt.Errorf("resume %s: %w", r.ResumeID, err)
		}
		r.session = s
		return nil
	default:
		if _, err := os.Stat(sessionsDir); err == nil {
			if s, err := LatestSession(sessionsDir); err == nil {
				r.session = s
				return nil
			}
		}
	}
	id := NewSessionID()
	r.session = &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: filepath.Join(sessionsDir, id),
		SpecPath:   filepath.Join(r.Wd, ".aios", "project.md"),
	}
	return r.session.Save()
}

func runAutopilotShip(ctx context.Context, wd string) error {
	_, err := ShipSpec(ctx, wd)
	return err
}
