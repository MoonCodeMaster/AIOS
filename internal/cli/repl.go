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

	"github.com/MoonCodeMaster/AIOS/internal/config"
	"github.com/MoonCodeMaster/AIOS/internal/engine"
	"github.com/MoonCodeMaster/AIOS/internal/engine/prompts"
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

	fmt.Fprintln(r.Out, "aios — type a requirement, blank line to submit. /help for commands.")
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
	id := NewSessionID()
	r.session = &Session{
		ID:         id,
		Created:    time.Now().UTC(),
		SessionDir: filepath.Join(r.Wd, ".aios", "sessions", id),
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
	in := specgen.Input{
		UserRequest: msg,
		PriorTurns:  prior,
		CurrentSpec: currentSpec,
		Claude:      r.Claude,
		Codex:       r.Codex,
		Recorder:    rec,
		OnStageStart: func(name string) {
			r.outMu.Lock()
			fmt.Fprintf(r.Out, "  · %s …\n", name)
			r.outMu.Unlock()
		},
		OnStageEnd: func(_ string, _ error) {},
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
func runAutopilotShip(_ context.Context, wd string) error {
	if err := decomposeOnly(wd); err != nil {
		return fmt.Errorf("decompose: %w", err)
	}
	runCmd := newRunCmd()
	if err := runCmd.Flags().Set("autopilot", "true"); err != nil {
		return fmt.Errorf("set --autopilot: %w", err)
	}
	if err := runCmd.Flags().Set("merge", "true"); err != nil {
		return fmt.Errorf("set --merge: %w", err)
	}
	return runMain(runCmd, nil)
}

// decomposeOnly turns the existing .aios/project.md into task files,
// reusing the same decompose prompt as `aios new` but skipping
// brainstorm and spec-synth (the spec is already final).
func decomposeOnly(wd string) error {
	specPath := filepath.Join(wd, ".aios", "project.md")
	specBody, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read project.md: %w", err)
	}
	cfg, err := config.Load(filepath.Join(wd, ".aios", "config.toml"))
	if err != nil {
		return err
	}
	codex := &engine.CodexEngine{
		Binary:     cfg.Engines.Codex.Binary,
		ExtraArgs:  cfg.Engines.Codex.ExtraArgs,
		TimeoutSec: cfg.Engines.Codex.TimeoutSec,
	}
	dPrompt, err := prompts.Render("decompose.tmpl", map[string]string{"Spec": string(specBody)})
	if err != nil {
		return err
	}
	dRes, err := codex.Invoke(context.Background(), engine.InvokeRequest{Role: engine.RoleCoder, Prompt: dPrompt})
	if err != nil {
		return err
	}
	tasksDir := filepath.Join(wd, ".aios", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	if _, err := writeTaskFiles(tasksDir, dRes.Text); err != nil {
		return err
	}
	return commitNewSpec(wd, cfg.Project.StagingBranch, "interactive session")
}

// readMessage reads lines until a blank line (submit) or EOF.
func readMessage(s *bufio.Scanner, out io.Writer) (string, bool) {
	fmt.Fprint(out, "> ")
	var lines []string
	for s.Scan() {
		line := s.Text()
		if line == "" {
			return strings.Join(lines, "\n"), true
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}
