package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// App is the top-level bubbletea model for the AIOS interactive TUI.
// Layout replicates the Codex CLI experience:
//
//	┌─────────────────────────────────────────┐
//	│  header (brand + model + session info)   │
//	│  ─────────────────────────────────────── │
//	│  scrollable chat history                 │
//	│  (user msgs + AI responses + tool calls) │
//	│  ─────────────────────────────────────── │
//	│  [status indicator: Working... 5s]       │
//	│  [slash command popup]                   │
//	│  input composer (textarea)               │
//	│  footer (dynamic key hints)              │
//	└─────────────────────────────────────────┘
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

	// Slash command popup.
	slashPopup    bool
	slashFilter   string
	slashSelected int
	slashMatches  []SlashCommand

	// Pipeline stage tracking.
	stages      []stageState
	stageOrder  []string
	shimmerTick int

	// Streaming response state.
	streaming       bool
	streamBuf       strings.Builder
	streamStartTime time.Time

	// State.
	waiting   bool   // true while specgen is running
	sessionID string
	turnCount int
	version   string
	specLines int
	model     string
	err       error

	// Callbacks — set by the REPL wiring layer.
	OnSubmit func(msg string) tea.Cmd // called when user submits input
	OnShip   func() tea.Cmd          // called on /ship
	OnExit   func()                  // called on /exit or Ctrl+C

	// Markdown renderer.
	mdRenderer *glamour.TermRenderer

	// Last AI response raw text (for /copy).
	lastAIRaw string
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
	ta.Placeholder = "Type a message..."
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

	app := App{
		width:      80,
		height:     24,
		input:      ta,
		viewport:   vp,
		version:    version,
		sessionID:  sessionID,
		turnCount:  turnCount,
		model:      "claude+codex",
		mdRenderer: md,
	}

	// Show a welcome hint for new sessions.
	if turnCount == 0 {
		app.history = append(app.history, chatEntry{
			Role: "system",
			Content: styleDim.Render("  Type a requirement to generate a spec. Use ") +
				styleCmd.Render("/") + styleDim.Render(" for commands."),
		})
	}

	return app
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
		// Triggers re-render.

	case streamChunkMsg:
		a.streaming = true
		a.streamBuf.WriteString(msg.Text)

	case streamDoneMsg:
		if a.streaming {
			a.streaming = false
			content := a.streamBuf.String()
			a.streamBuf.Reset()
			if content != "" {
				a.AppendAI(content)
			}
		}

	case specDoneMsg:
		a.waiting = false
		a.stages = nil
		a.stageOrder = nil
		if msg.Err != nil {
			a.err = msg.Err
			errStr := msg.Err.Error()
			// Make common errors more actionable.
			switch {
			case strings.Contains(errStr, "executable file not found"):
				a.appendSystem(styleError.Render("✗") + " Engine not found. Install the missing CLI:\n" +
					"    " + styleCmd.Render("npm i -g @anthropic-ai/claude-code") + "  (claude)\n" +
					"    " + styleCmd.Render("npm i -g @openai/codex") + "            (codex)\n" +
					"  Then run " + styleCmd.Render("aios doctor") + " to verify.")
			case strings.Contains(errStr, "timed out"):
				a.appendSystem(styleError.Render("✗") + " Engine timed out. Check your network and API auth.")
			default:
				a.appendSystem(styleError.Render("✗") + " " + errStr)
			}
		} else {
			for _, w := range msg.Warnings {
				a.appendSystem(styleWarn.Render("⚠") + " " + w)
			}
			a.specLines = msg.Lines
			a.appendSystem(fmt.Sprintf(
				"%s Spec updated (%d lines). %s to view, %s to implement, or refine.",
				styleSuccess.Render("✓"),
				msg.Lines,
				styleCmd.Render("/show"),
				styleCmd.Render("/ship"),
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

	// Update slash popup state based on input.
	a.updateSlashPopup()

	a.rebuildViewport()
	a.viewport, cmd = a.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Slash popup navigation.
	if a.slashPopup {
		switch msg.Type {
		case tea.KeyUp:
			if a.slashSelected > 0 {
				a.slashSelected--
			}
			return nil
		case tea.KeyDown:
			if a.slashSelected < len(a.slashMatches)-1 {
				a.slashSelected++
			}
			return nil
		case tea.KeyTab, tea.KeyEnter:
			if len(a.slashMatches) > 0 {
				selected := a.slashMatches[a.slashSelected]
				a.input.SetValue("/" + selected.Name + " ")
				a.slashPopup = false
				a.slashMatches = nil
			}
			return nil
		case tea.KeyEsc:
			a.slashPopup = false
			return nil
		}
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if a.waiting {
			// Interrupt current operation.
			a.waiting = false
			a.streaming = false
			a.streamBuf.Reset()
			a.stages = nil
			a.appendSystem(styleDim.Render("⎋ Interrupted."))
			a.input.Focus()
			return nil
		}
		if a.OnExit != nil {
			a.OnExit()
		}
		return tea.Quit

	case tea.KeyEsc:
		if a.waiting {
			// Interrupt.
			a.waiting = false
			a.streaming = false
			a.streamBuf.Reset()
			a.stages = nil
			a.appendSystem(styleDim.Render("⎋ Interrupted."))
			a.input.Focus()
			return nil
		}
		if a.OnExit != nil {
			a.OnExit()
		}
		return tea.Quit

	case tea.KeyEnter:
		if a.waiting {
			return nil
		}
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

	case tea.KeyPgUp:
		a.viewport.HalfViewUp()
		return nil

	case tea.KeyPgDown:
		a.viewport.HalfViewDown()
		return nil

	case tea.KeyCtrlU:
		a.viewport.HalfViewUp()
		return nil

	case tea.KeyCtrlD:
		a.viewport.HalfViewDown()
		return nil
	}
	return nil
}

func (a *App) submit(val string) tea.Cmd {
	a.slashPopup = false
	a.slashMatches = nil

	// Save to history.
	a.inputHistory = append(a.inputHistory, val)
	a.historyIdx = len(a.inputHistory)
	a.input.Reset()

	// Handle slash commands.
	if strings.HasPrefix(val, "/") {
		return a.handleSlashCommand(val)
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

func (a *App) handleSlashCommand(val string) tea.Cmd {
	parts := strings.Fields(val)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/exit", "/quit":
		if a.OnExit != nil {
			a.OnExit()
		}
		return tea.Quit

	case "/help":
		a.appendSystem(a.renderHelp())
		return nil

	case "/show":
		if a.OnSubmit != nil {
			return a.OnSubmit(val)
		}
		return nil

	case "/clear":
		a.history = nil
		a.appendSystem(styleSuccess.Render("✓") + " Conversation cleared.")
		if a.OnSubmit != nil {
			return a.OnSubmit(val)
		}
		return nil

	case "/ship":
		a.appendUser(val)
		a.appendSystem(styleInfo.Render("🚀") + " Shipping spec to autopilot…")
		if a.OnShip != nil {
			return a.OnShip()
		}
		return nil

	case "/model":
		a.appendSystem(fmt.Sprintf("  Model: %s", styleBold.Render(a.model)))
		return nil

	case "/status":
		a.appendSystem(a.renderStatus())
		return nil

	case "/diff":
		if a.OnSubmit != nil {
			return a.OnSubmit(val)
		}
		return nil

	case "/compact":
		a.appendSystem(styleDim.Render("  Compacting conversation history…"))
		if a.OnSubmit != nil {
			return a.OnSubmit(val)
		}
		return nil

	case "/copy":
		if a.lastAIRaw == "" {
			a.appendSystem(styleWarn.Render("⚠") + " Nothing to copy.")
		} else if err := clipboard.WriteAll(a.lastAIRaw); err != nil {
			a.appendSystem(styleError.Render("✗") + " Copy failed: " + err.Error())
		} else {
			a.appendSystem(styleSuccess.Render("✓") + " Last response copied to clipboard.")
		}
		return nil

	case "/new":
		a.history = nil
		a.turnCount = 0
		a.appendSystem(styleSuccess.Render("✓") + " New conversation started.")
		return nil

	case "/rename":
		if len(parts) > 1 {
			a.sessionID = strings.Join(parts[1:], " ")
			a.appendSystem(styleSuccess.Render("✓") + " Session renamed to: " + a.sessionID)
		} else {
			a.appendSystem(styleWarn.Render("⚠") + " Usage: /rename <name>")
		}
		return nil

	default:
		a.appendSystem(styleWarn.Render("⚠") + " Unknown command: " + cmd + ". Type " + styleCmd.Render("/help") + " for available commands.")
		return nil
	}
}

// View implements tea.Model.
func (a App) View() string {
	header := a.renderHeader()
	footer := a.renderFooter()
	stages := a.renderStages()
	status := a.renderStatusIndicator()
	popup := a.renderSlashPopup()

	// Calculate available height.
	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	stagesH := lipgloss.Height(stages)
	statusH := lipgloss.Height(status)
	popupH := lipgloss.Height(popup)
	inputH := 5
	chatH := a.height - headerH - footerH - stagesH - statusH - popupH - inputH - 1
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
	if status != "" {
		sections = append(sections, status)
	}
	if popup != "" {
		sections = append(sections, popup)
	}
	sections = append(sections, composer)
	sections = append(sections, footer)

	return strings.Join(sections, "\n")
}

// --- Rendering helpers ---

func (a App) renderHeader() string {
	brand := styleBoldCyan.Render("  aios")
	ver := styleDim.Render(" v" + a.version)
	model := styleDim.Render(" · ") + styleModel.Render(a.model)
	var session string
	if a.sessionID != "" {
		session = styleDim.Render(fmt.Sprintf(" · session: %s", a.sessionID))
	}
	if a.turnCount > 0 {
		session += styleDim.Render(fmt.Sprintf(" (%d turns)", a.turnCount))
	}
	line := brand + ver + model + session
	sep := styleDim.Render(strings.Repeat("─", a.width))
	return line + "\n" + sep
}

func (a App) renderFooter() string {
	sep := styleDim.Render(strings.Repeat("─", a.width))
	var hints []string
	if a.waiting {
		hints = append(hints,
			styleKey.Render("esc")+" interrupt",
			styleKey.Render("ctrl+c")+" quit",
		)
	} else {
		hints = append(hints,
			styleKey.Render("enter")+" submit",
			styleKey.Render("/")+" commands",
			styleKey.Render("↑↓")+" history",
			styleKey.Render("pgup/dn")+" scroll",
			styleKey.Render("esc")+" quit",
		)
	}
	bar := "  " + strings.Join(hints, styleDim.Render("  ·  "))
	return sep + "\n" + bar
}

func (a App) renderStatusIndicator() string {
	if !a.waiting {
		return ""
	}
	elapsed := time.Duration(0)
	for _, s := range a.stages {
		if !s.done {
			elapsed = time.Since(s.started)
			break
		}
	}
	if elapsed == 0 && a.waiting {
		elapsed = time.Duration(a.shimmerTick) * 80 * time.Millisecond
	}
	frame := shimmerFrames[a.shimmerTick%len(shimmerFrames)]
	indicator := styleStageActive.Render(frame) + " " + styleDim.Render("Working…") + " " + styleStageTime.Render(formatElapsed(elapsed))
	if len(a.stages) > 0 {
		for _, s := range a.stages {
			if !s.done {
				indicator += styleDim.Render(" · ") + s.name
				break
			}
		}
	}
	return "  " + indicator
}

func (a App) renderStages() string {
	if len(a.stages) == 0 {
		return ""
	}
	var lines []string
	for _, s := range a.stages {
		if s.done {
			if s.err != nil {
				lines = append(lines, fmt.Sprintf("  %s %s %s",
					styleStageFail.Render("✗"),
					s.name,
					styleStageFail.Render(s.err.Error()),
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
			frame := shimmerFrames[a.shimmerTick%len(shimmerFrames)]
			lines = append(lines, fmt.Sprintf("  %s %s %s",
				styleStageActive.Render(frame),
				s.name,
				styleStageTime.Render(formatElapsed(elapsed)),
			))
		}
	}
	return strings.Join(lines, "\n")
}

func (a App) renderComposer() string {
	prompt := stylePrompt.Render("❯ ")
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

func (a App) renderSlashPopup() string {
	if !a.slashPopup || len(a.slashMatches) == 0 {
		return ""
	}
	var lines []string
	maxShow := 8
	if len(a.slashMatches) < maxShow {
		maxShow = len(a.slashMatches)
	}
	for i := 0; i < maxShow; i++ {
		sc := a.slashMatches[i]
		name := styleCmd.Render("/" + sc.Name)
		desc := styleDim.Render(" — " + sc.Description)
		prefix := "  "
		if i == a.slashSelected {
			prefix = styleSelected.Render("▸ ")
		}
		lines = append(lines, prefix+name+desc)
	}
	if len(a.slashMatches) > maxShow {
		lines = append(lines, styleDim.Render(fmt.Sprintf("  … and %d more", len(a.slashMatches)-maxShow)))
	}
	return strings.Join(lines, "\n")
}

func (a App) renderStatus() string {
	var lines []string
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s %s", styleDim.Render("Model:"), styleBold.Render(a.model)))
	lines = append(lines, fmt.Sprintf("  %s %s", styleDim.Render("Session:"), a.sessionID))
	lines = append(lines, fmt.Sprintf("  %s %d", styleDim.Render("Turns:"), a.turnCount))
	if a.specLines > 0 {
		lines = append(lines, fmt.Sprintf("  %s %d lines", styleDim.Render("Spec:"), a.specLines))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func (a App) renderHelp() string {
	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+styleBold.Render("Commands:"))
	lines = append(lines, "")
	for _, sc := range AllSlashCommands {
		lines = append(lines, fmt.Sprintf("    %s  %s", styleCmd.Render(fmt.Sprintf("%-12s", "/"+sc.Name)), styleDim.Render(sc.Description)))
	}
	lines = append(lines, "")
	lines = append(lines, "  "+styleDim.Render("Tips:"))
	lines = append(lines, "    "+styleDim.Render("Enter submits. Shift+Enter for newlines."))
	lines = append(lines, "    "+styleDim.Render("↑/↓ arrows navigate input history."))
	lines = append(lines, "    "+styleDim.Render("Type / to see command autocomplete."))
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

// --- Slash popup logic ---

func (a *App) updateSlashPopup() {
	val := a.input.Value()
	if strings.HasPrefix(val, "/") && !a.waiting {
		filter := strings.TrimPrefix(val, "/")
		filter = strings.ToLower(strings.Fields(filter+" ")[0]) // first word only
		matches := filterSlashCommands(filter)
		if len(matches) > 0 && val != "/"+matches[0].Name+" " {
			a.slashPopup = true
			a.slashFilter = filter
			a.slashMatches = matches
			if a.slashSelected >= len(matches) {
				a.slashSelected = 0
			}
		} else {
			a.slashPopup = false
			a.slashMatches = nil
		}
	} else {
		a.slashPopup = false
		a.slashMatches = nil
	}
}

// --- Chat history management ---

func (a *App) appendUser(msg string) {
	rendered := styleUserLabel.Render("You: ") + styleUserMsg.Render(msg)
	a.history = append(a.history, chatEntry{Role: "user", Content: rendered})
}

func (a *App) appendSystem(msg string) {
	a.history = append(a.history, chatEntry{Role: "system", Content: msg})
}

// AppendAI adds a markdown-rendered AI response to the chat history.
func (a *App) AppendAI(md string) {
	a.lastAIRaw = md
	rendered := md
	if a.mdRenderer != nil {
		if out, err := a.mdRenderer.Render(md); err == nil {
			rendered = strings.TrimSpace(out)
		}
	}
	a.history = append(a.history, chatEntry{Role: "ai", Content: rendered})
}

func (a *App) rebuildViewport() {
	var lines []string
	for _, e := range a.history {
		lines = append(lines, e.Content)
		lines = append(lines, "")
	}
	// If streaming, show partial content.
	if a.streaming && a.streamBuf.Len() > 0 {
		partial := a.streamBuf.String()
		lines = append(lines, styleDim.Render(partial))
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

func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm %02ds", m, s)
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

// StreamChunk sends a streaming text chunk to the TUI.
func StreamChunk(text string) tea.Msg { return streamChunkMsg{Text: text} }

// StreamDone signals the end of a streaming response.
func StreamDone() tea.Msg { return streamDoneMsg{} }
