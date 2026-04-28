package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/run"
	"github.com/MoonCodeMaster/AIOS/internal/specgen"
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
	outMu   sync.Mutex // guards Out against concurrent stage callbacks
}

// Run executes the REPL turn loop until /exit, EOF, or /ship.
func (r *Repl) Run(ctx context.Context) error {
	if r.LookPath == nil {
		r.LookPath = exec.LookPath
	}
	if r.ClaudeBinary != "" {
		if _, err := r.LookPath(r.ClaudeBinary); err != nil {
			return fmt.Errorf("claude CLI not found (%s): run `aios doctor`", r.ClaudeBinary)
		}
	}
	if r.CodexBinary != "" {
		if _, err := r.LookPath(r.CodexBinary); err != nil {
			return fmt.Errorf("codex CLI not found (%s): run `aios doctor`", r.CodexBinary)
		}
	}
	if err := r.bootSession(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(r.In)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // long pasted prompts

	fmt.Fprintln(r.Out, `aios — type a requirement and press Enter. End a line with "\" or wrap in """…""" for multi-line. /help for commands.`)
	for {
		msg, ok := readMessage(scanner, r.Out)
		if !ok {
			return nil
		}
		switch ParseSlash(msg) {
		case SlashExit:
			fmt.Fprintln(r.Out, "bye.")
			return nil
		case SlashHelp:
			r.printHelp()
			continue
		case SlashShow:
			r.printSpec()
			continue
		case SlashClear:
			r.session.Turns = nil
			_ = r.session.Save()
			fmt.Fprintln(r.Out, "session cleared.")
			continue
		case SlashShip:
			return r.ship(ctx)
		case SlashUnknown:
			fmt.Fprintf(r.Out, "unknown slash command. /help for the list.\n")
			continue
		}
		// Natural-language input → run the pipeline.
		if err := r.runTurn(ctx, msg); err != nil {
			fmt.Fprintf(r.Out, "turn failed: %v\n", err)
		}
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
		fmt.Fprintf(r.Out, "resumed session %s (%d prior turns)\n", s.ID, len(s.Turns))
		return nil
	default:
		// Auto-resume the latest session if any exist.
		if _, err := os.Stat(sessionsDir); err == nil {
			if s, err := LatestSession(sessionsDir); err == nil {
				r.session = s
				fmt.Fprintf(r.Out, "resumed session %s (%d prior turns)\n", s.ID, len(s.Turns))
				return nil
			}
		}
	}
	// Fresh session.
	id := NewSessionID()
	r.session = &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: filepath.Join(sessionsDir, id),
		SpecPath:   filepath.Join(r.Wd, ".aios", "project.md"),
	}
	return r.session.Save()
}

func (r *Repl) runTurn(ctx context.Context, msg string) error {
	runID := time.Now().UTC().Format("2006-01-02T15-04-05")
	rec, err := run.Open(filepath.Join(r.Wd, ".aios", "runs"), runID)
	if err != nil {
		return err
	}
	currentSpec := ""
	if data, err := os.ReadFile(r.session.SpecPath); err == nil {
		currentSpec = string(data)
	}
	prior := make([]specgen.Turn, len(r.session.Turns))
	for i, t := range r.session.Turns {
		prior[i] = specgen.Turn{UserMessage: t.UserMessage, FinalSpec: t.SpecAfter}
	}
	stageStart := make(map[string]time.Time)
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
			r.outMu.Lock()
			stageStart[name] = time.Now()
			fmt.Fprintf(r.Out, "  · %s …\n", name)
			r.outMu.Unlock()
		},
		OnStageEnd: func(name string, err error) {
			r.outMu.Lock()
			defer r.outMu.Unlock()
			elapsed := time.Since(stageStart[name]).Round(time.Millisecond)
			if err != nil {
				fmt.Fprintf(r.Out, "  ✗ %s failed in %s: %v\n", name, elapsed, err)
				return
			}
			fmt.Fprintf(r.Out, "  ✓ %s (%s)\n", name, elapsed)
		},
	}
	out, err := specgen.Generate(ctx, in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.session.SpecPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(r.session.SpecPath, []byte(out.Final), 0o644); err != nil {
		return err
	}
	r.session.Turns = append(r.session.Turns, SessionTurn{
		Timestamp: time.Now().UTC(), UserMessage: msg, SpecAfter: out.Final, RunID: runID,
	})
	if err := r.session.Save(); err != nil {
		return err
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(r.Out, "  ! %s\n", w)
	}
	lineCount := strings.Count(out.Final, "\n") + 1
	fmt.Fprintf(r.Out, "Spec updated (%d lines). /show to view, /ship to implement, or refine with another message.\n", lineCount)
	return nil
}

func (r *Repl) printSpec() {
	data, err := os.ReadFile(r.session.SpecPath)
	if err != nil {
		fmt.Fprintf(r.Out, "no spec yet.\n")
		return
	}
	fmt.Fprintln(r.Out, "---")
	fmt.Fprintln(r.Out, string(data))
	fmt.Fprintln(r.Out, "---")
}

func (r *Repl) printHelp() {
	fmt.Fprintln(r.Out, `input: Enter submits. End a line with "\" to continue, or wrap in """…""" for multi-line.`)
	fmt.Fprintln(r.Out, "commands:")
	fmt.Fprintln(r.Out, "  /show   print current spec")
	fmt.Fprintln(r.Out, "  /clear  discard session, start fresh")
	fmt.Fprintln(r.Out, "  /ship   hand the spec to autopilot (decompose → run → PR)")
	fmt.Fprintln(r.Out, "  /exit   leave the REPL")
	fmt.Fprintln(r.Out, "  /help   this list")
}

func (r *Repl) ship(ctx context.Context) error {
	if r.ShipFn == nil {
		r.ShipFn = runAutopilotShip
	}
	fmt.Fprintln(r.Out, "shipping spec to autopilot…")
	return r.ShipFn(ctx, r.Wd)
}

// runAutopilotShip drives `aios run --autopilot --merge` against the spec
// already on disk at <wd>/.aios/project.md. Equivalent to typing `aios
// autopilot` after `aios new --auto` has run.
func runAutopilotShip(ctx context.Context, wd string) error {
	_, err := ShipSpec(ctx, wd)
	return err
}

// readMessage reads one user prompt from stdin.
//
// A single Enter submits the line — matching the UX of codex and claude CLIs.
// For typed multi-line input, two affordances are supported:
//   - end a line with "\" to continue on the next line (the trailing "\" is dropped); or
//   - open the prompt with a line containing only `"""` and close it with another `"""` line.
//
// A bare Enter on the primary prompt re-prompts without doing anything.
func readMessage(s *bufio.Scanner, out io.Writer) (string, bool) {
	for {
		fmt.Fprint(out, "> ")
		if !s.Scan() {
			return "", false
		}
		first := s.Text()
		if first == "" {
			continue
		}
		if strings.TrimSpace(first) == `"""` {
			var lines []string
			for {
				fmt.Fprint(out, ".. ")
				if !s.Scan() {
					return strings.Join(lines, "\n"), true
				}
				line := s.Text()
				if strings.TrimSpace(line) == `"""` {
					return strings.Join(lines, "\n"), true
				}
				lines = append(lines, line)
			}
		}
		if strings.HasSuffix(first, `\`) {
			lines := []string{strings.TrimSuffix(first, `\`)}
			for {
				fmt.Fprint(out, ".. ")
				if !s.Scan() {
					return strings.Join(lines, "\n"), true
				}
				cont := s.Text()
				if strings.HasSuffix(cont, `\`) {
					lines = append(lines, strings.TrimSuffix(cont, `\`))
					continue
				}
				lines = append(lines, cont)
				return strings.Join(lines, "\n"), true
			}
		}
		return first, true
	}
}
