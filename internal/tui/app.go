package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// App is the top-level bubbletea model for the AIOS interactive TUI.
// Layout (matching Codex CLI):
//
//	┌─────────────────────────────┐
//	│  header (brand + session)   │
//	│  ─────────────────────────  │
//	│  scrollable chat history    │
//	│  (user msgs + AI responses) │
//	│  stage progress indicators  │
//	│  ─────────────────────────  │
//	│  input composer (textarea)  │
//	│  footer (key hints)         │
//	└─────────────────────────────┘
type App struct {
	// Dimensions.
	width, height int

	// Chat history.
	history  []chatEntry
	viewport viewport.Model

	// Input composer.
	input        textarea.Model
	inputHistory []string
	historyIdx   int

	// Pipeline stage tracking.
	stages      []stageState
	stageOrder  []string
	shimmerTick int

	// State.
	waiting   bool   // true while specgen is running
	sessionID string
	turnCount int
	version   string
	specLines int
	err       error

	// Callbacks — set by the REPL wiring layer.
	OnSubmit func(msg string) tea.Cmd // called when user submits input
	OnShip   func() tea.Cmd          // called on /ship
	OnExit   func()                  // called on /exit or Ctrl+C

	// Markdown renderer.
	mdRenderer *glamour.TermRenderer
}

type stageState struct {
	name    string
	started time.Time
	elapsed time.Duration
	done    bool
	err     error
}

// New creates a new App model.
func New(version, sessionID string, turnCount int) App {
	ta := textarea.New()
	ta.Placeholder = "Type a requirement and press Enter..."
	ta.CharLimit = 0 // unlimited
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.Focus()
	ta.ShowLineNumbers = false

	vp := viewport.New(80, 20)

	md, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(78),
	)

	return App{
		width:      80,
		height:     24,
		input:      ta,
		viewport:   vp,
		version:    version,
		sessionID:  sessionID,
		turnCount:  turnCount,
		mdRenderer: md,
	}
}

// Init implements tea.Model.
func (a App) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.recalcLayout()
		// Recreate markdown renderer with new width.
		w := a.width - 4
		if w < 40 {
			w = 40
		}
		a.mdRenderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(w),
		)

	case tea.KeyMsg:
		cmd := a.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tickMsg:
		a.shimmerTick++
		cmds = append(cmds, tickCmd())

	case stageStartMsg:
		a.stages = append(a.stages, stageState{name: msg.Name, started: time.Now()})
		a.stageOrder = append(a.stageOrder, msg.Name)

	case stageEndMsg:
		for i := range a.stages {
			if a.stages[i].name == msg.Name {
				a.stages[i].done = true
				a.stages[i].elapsed = msg.Elapsed
				a.stages[i].err = msg.Err
				break
			}
		}

	case stageProgressMsg:
		// Just triggers a re-render (shimmer tick handles animation).

	case specDoneMsg:
		a.waiting = false
		a.stages = nil
		a.stageOrder = nil
		if msg.Err != nil {
			a.err = msg.Err
			a.appendSystem(styleError.Render("✗") + " turn failed: " + msg.Err.Error())
		} else {
			for _, w := range msg.Warnings {
				a.appendSystem(styleWarn.Render("⚠") + " " + w)
			}
			a.specLines = msg.Lines
			a.appendSystem(fmt.Sprintf(
				"%s Spec updated (%d lines). %s to view, %s to implement, or refine.",
				styleSuccess.Render("✓"),
				msg.Lines,
				styleInfo.Render("/show"),
				styleInfo.Render("/ship"),
			))
			a.turnCount++
		}
		a.input.Focus()
	}

	// Update sub-models.
	var cmd tea.Cmd
	if !a.waiting {
		a.input, cmd = a.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	a.rebuildViewport()
	a.viewport, cmd = a.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC:
		if a.OnExit != nil {
			a.OnExit()
		}
		return tea.Quit

	case tea.KeyEsc:
		if a.waiting {
			return nil // can't exit while waiting
		}
		if a.OnExit != nil {
			a.OnExit()
		}
		return tea.Quit

	case tea.KeyEnter:
		if a.waiting {
			return nil
		}
		// Shift+Enter or Alt+Enter for newline (handled by textarea).
		// Plain Enter submits.
		val := strings.TrimSpace(a.input.Value())
		if val == "" {
			return nil
		}
		return a.submit(val)

	case tea.KeyUp:
		if !a.waiting && a.input.Value() == "" && len(a.inputHistory) > 0 {
			if a.historyIdx > 0 {
				a.historyIdx--
				a.input.SetValue(a.inputHistory[a.historyIdx])
			}
			return nil
		}

	case tea.KeyDown:
		if !a.waiting && len(a.inputHistory) > 0 {
			if a.historyIdx < len(a.inputHistory)-1 {
				a.historyIdx++
				a.input.SetValue(a.inputHistory[a.historyIdx])
			} else {
				a.historyIdx = len(a.inputHistory)
				a.input.SetValue("")
			}
			return nil
		}
	}
	return nil
}

func (a *App) submit(val string) tea.Cmd {
	// Save to history.
	a.inputHistory = append(a.inputHistory, val)
	a.historyIdx = len(a.inputHistory)
	a.input.Reset()

	// Handle slash commands.
	switch {
	case val == "/exit" || val == "/quit":
		if a.OnExit != nil {
			a.OnExit()
		}
		return tea.Quit

	case val == "/help":
		a.appendSystem(a.renderHelp())
		return nil

	case val == "/show":
		// Handled by the wiring layer via OnSubmit.
		if a.OnSubmit != nil {
			return a.OnSubmit(val)
		}
		return nil

	case val == "/clear":
		a.history = nil
		a.appendSystem(styleSuccess.Render("✓") + " Session cleared.")
		if a.OnSubmit != nil {
			return a.OnSubmit(val)
		}
		return nil

	case val == "/ship":
		a.appendUser(val)
		a.appendSystem(styleInfo.Render("🚀") + " Shipping spec to autopilot…")
		if a.OnShip != nil {
			return a.OnShip()
		}
		return nil

	default:
		if strings.HasPrefix(val, "/") {
			a.appendSystem(styleWarn.Render("⚠") + " Unknown slash command. /help for the list.")
			return nil
		}
	}

	// Normal message — run specgen.
	a.appendUser(val)
	a.waiting = true
	a.err = nil
	a.input.Blur()
	if a.OnSubmit != nil {
		return a.OnSubmit(val)
	}
	return nil
}

// View implements tea.Model.
func (a App) View() string {
	header := a.renderHeader()
	footer := a.renderFooter()
	stages := a.renderStages()

	// Calculate available height.
	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	stagesH := lipgloss.Height(stages)
	inputH := 5 // textarea + border
	chatH := a.height - headerH - footerH - stagesH - inputH - 1
	if chatH < 3 {
		chatH = 3
	}
	a.viewport.Height = chatH
	a.viewport.Width = a.width

	composer := a.renderComposer()

	var sections []string
	sections = append(sections, header)
	sections = append(sections, a.viewport.View())
	if stages != "" {
		sections = append(sections, stages)
	}
	sections = append(sections, composer)
	sections = append(sections, footer)

	return strings.Join(sections, "\n")
}

// --- Rendering helpers ---

func (a App) renderHeader() string {
	brand := styleBoldCyan.Render("  aios")
	ver := styleDim.Render(" v" + a.version)
	var session string
	if a.sessionID != "" {
		session = styleDim.Render(fmt.Sprintf("  session: %s (%d turns)", a.sessionID, a.turnCount))
	}
	line := brand + ver + session
	sep := styleDim.Render(strings.Repeat("─", a.width))
	return line + "\n" + sep
}

func (a App) renderFooter() string {
	sep := styleDim.Render(strings.Repeat("─", a.width))
	var hints []string
	if a.waiting {
		hints = append(hints, styleKey.Render("ctrl+c")+" quit")
	} else {
		hints = append(hints,
			styleKey.Render("enter")+" submit",
			styleKey.Render("/ship")+" deploy",
			styleKey.Render("/show")+" view spec",
			styleKey.Render("/help")+" commands",
			styleKey.Render("esc")+" quit",
		)
	}
	bar := styleFooter.Render("  " + strings.Join(hints, styleDim.Render("  ·  ")))
	return sep + "\n" + bar
}

func (a App) renderStages() string {
	if len(a.stages) == 0 {
		return ""
	}
	var lines []string
	for _, s := range a.stages {
		if s.done {
			if s.err != nil {
				lines = append(lines, fmt.Sprintf("  %s %s %s %s",
					styleStageFail.Render("✗"),
					s.name,
					styleStageFail.Render(fmt.Sprintf("failed in %s:", s.elapsed.Round(time.Millisecond))),
					s.err,
				))
			} else {
				lines = append(lines, fmt.Sprintf("  %s %s %s",
					styleStageDone.Render("✓"),
					s.name,
					styleStageTime.Render(fmt.Sprintf("(%s)", s.elapsed.Round(time.Millisecond))),
				))
			}
		} else {
			elapsed := time.Since(s.started)
			spinner := a.shimmerText(s.name)
			lines = append(lines, fmt.Sprintf("  %s %s",
				spinner,
				styleStageTime.Render(formatElapsed(elapsed)),
			))
		}
	}
	return strings.Join(lines, "\n")
}

func (a App) renderComposer() string {
	prompt := styleInfo.Render("❯ ")
	if a.waiting {
		prompt = styleDim.Render("  ")
	}
	w := a.width - 4
	if w < 20 {
		w = 20
	}
	a.input.SetWidth(w)
	box := styleComposerBorder.Width(a.width - 2).Render(prompt + a.input.View())
	return box
}

func (a App) renderHelp() string {
	return fmt.Sprintf(`
  %s

  %s
    %s   print current spec
    %s  discard session, start fresh
    %s   hand the spec to autopilot
    %s   leave the REPL
    %s   this list

  %s  Enter submits. Use shift+enter for newlines.
  %s  Up/Down arrows navigate input history.`,
		styleBold.Render("Commands:"),
		"",
		styleInfo.Render("/show"),
		styleInfo.Render("/clear"),
		styleInfo.Render("/ship"),
		styleInfo.Render("/exit"),
		styleInfo.Render("/help"),
		styleDim.Render("Tip:"),
		styleDim.Render("Tip:"),
	)
}

// --- Chat history management ---

func (a *App) appendUser(msg string) {
	rendered := styleUserMsg.Render(msg)
	a.history = append(a.history, chatEntry{Role: "user", Content: rendered})
}

func (a *App) appendSystem(msg string) {
	a.history = append(a.history, chatEntry{Role: "system", Content: msg})
}

// AppendAI adds a markdown-rendered AI response to the chat history.
func (a *App) AppendAI(md string) {
	rendered := md
	if a.mdRenderer != nil {
		if out, err := a.mdRenderer.Render(md); err == nil {
			rendered = strings.TrimSpace(out)
		}
	}
	a.history = append(a.history, chatEntry{Role: "ai", Content: styleAIMsg.Render(rendered)})
}

func (a *App) rebuildViewport() {
	var lines []string
	for _, e := range a.history {
		lines = append(lines, e.Content)
		lines = append(lines, "") // blank line between entries
	}
	content := strings.Join(lines, "\n")
	a.viewport.SetContent(content)
	a.viewport.GotoBottom()
}

func (a *App) recalcLayout() {
	w := a.width - 4
	if w < 40 {
		w = 40
	}
	a.input.SetWidth(w)
}

// --- Shimmer animation (matching Codex's shimmer.rs) ---

var shimmerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (a App) shimmerText(text string) string {
	frame := shimmerFrames[a.shimmerTick%len(shimmerFrames)]
	return styleStageActive.Render(frame + " " + text)
}

func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

// --- Public API for REPL wiring ---

// StageStart sends a stage-start message to the TUI.
func StageStart(name string) tea.Msg { return stageStartMsg{Name: name} }

// StageEnd sends a stage-end message to the TUI.
func StageEnd(name string, elapsed time.Duration, err error) tea.Msg {
	return stageEndMsg{Name: name, Elapsed: elapsed, Err: err}
}

// SpecDone sends a spec-done message to the TUI.
func SpecDone(final string, lines int, warnings []string, err error) tea.Msg {
	return specDoneMsg{Final: final, Lines: lines, Warnings: warnings, Err: err}
}
