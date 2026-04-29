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

	r.printWelcome()
	for {
		if ctx.Err() != nil {
			return nil
		}
		msg, ok := readMessage(scanner, r.Out)
		if !ok {
			return nil
		}
		switch ParseSlash(msg) {
		case SlashExit:
			cDim.Fprintln(r.Out, "bye. 👋")
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
			printSuccess(r.Out, "Session cleared.")
			continue
		case SlashShip:
			return r.ship(ctx)
		case SlashUnknown:
			printWarn(r.Out, "Unknown slash command. /help for the list.")
			continue
		}
		if err := r.runTurn(ctx, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			printError(r.Out, "turn failed: %v", err)
		}
	}
}

func (r *Repl) printWelcome() {
	fmt.Fprintln(r.Out)
	cBoldCyan.Fprintf(r.Out, "  aios")
	cDim.Fprintf(r.Out, " v%s", Version)
	fmt.Fprintln(r.Out)
	cDim.Fprintln(r.Out, `  Type a requirement and press Enter. /help for commands. Ctrl+C to quit.`)
	if len(r.session.Turns) > 0 {
		cDim.Fprintf(r.Out, "  Resumed session %s (%d prior turns)\n", r.session.ID, len(r.session.Turns))
	}
	fmt.Fprintln(r.Out)
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
	ticker := newStageTicker(r.Out)
	printDim(r.Out, "Drafting spec with Claude + Codex in parallel — typically 30–90s.")
	in := specgen.Input{
		UserRequest:       msg,
		PriorTurns:        prior,
		CurrentSpec:       currentSpec,
		Claude:            r.Claude,
		Codex:             r.Codex,
		Recorder:          rec,
		CritiqueEnabled:   r.CritiqueEnabled,
		CritiqueThreshold: r.CritiqueThreshold,
		OnStageStart:      ticker.Start,
		OnStageEnd:        ticker.End,
		OnStageProgress:   ticker.Progress,
	}
	out, err := specgen.Generate(ctx, in)
	ticker.Stop()
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
		printWarn(r.Out, "%s", w)
	}
	lineCount := strings.Count(out.Final, "\n") + 1
	printSuccess(r.Out, "Spec updated (%d lines). %s to view, %s to implement, or refine with another message.",
		lineCount, cCyan.Sprint("/show"), cCyan.Sprint("/ship"))
	return nil
}

func (r *Repl) printSpec() {
	data, err := os.ReadFile(r.session.SpecPath)
	if err != nil {
		printWarn(r.Out, "No spec yet.")
		return
	}
	cDim.Fprintln(r.Out, "───")
	fmt.Fprintln(r.Out, string(data))
	cDim.Fprintln(r.Out, "───")
}

func (r *Repl) printHelp() {
	fmt.Fprintln(r.Out)
	cDim.Fprintln(r.Out, `  Input: Enter submits. End a line with "\" to continue, or wrap in """…""" for multi-line.`)
	fmt.Fprintln(r.Out)
	cBold.Fprintln(r.Out, "  Commands:")
	fmt.Fprintf(r.Out, "    %s   print current spec\n", cCyan.Sprint("/show"))
	fmt.Fprintf(r.Out, "    %s  discard session, start fresh\n", cCyan.Sprint("/clear"))
	fmt.Fprintf(r.Out, "    %s   hand the spec to autopilot (decompose → run → PR)\n", cCyan.Sprint("/ship"))
	fmt.Fprintf(r.Out, "    %s   leave the REPL\n", cCyan.Sprint("/exit"))
	fmt.Fprintf(r.Out, "    %s   this list\n", cCyan.Sprint("/help"))
	fmt.Fprintln(r.Out)
}

func (r *Repl) ship(ctx context.Context) error {
	if r.ShipFn == nil {
		r.ShipFn = runAutopilotShip
	}
	printInfo(r.Out, "🚀 Shipping spec to autopilot…")
	return r.ShipFn(ctx, r.Wd)
}

func runAutopilotShip(ctx context.Context, wd string) error {
	_, err := ShipSpec(ctx, wd)
	return err
}

// readMessage reads one user prompt from stdin.
func readMessage(s *bufio.Scanner, out io.Writer) (string, bool) {
	for {
		cCyan.Fprint(out, "❯ ")
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
				cDim.Fprint(out, "· ")
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
				cDim.Fprint(out, "· ")
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
