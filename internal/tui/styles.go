package tui

import "github.com/charmbracelet/lipgloss"

// Theme colors matching Codex CLI's style guide:
//   - Default: most text uses default foreground
//   - Cyan: user input tips, selection, status indicators
//   - Green: success and additions
//   - Red: errors, failures, deletions
//   - Magenta: AIOS branding (Codex uses magenta for "Codex")
//   - Dim: secondary text

var (
	colorCyan    = lipgloss.Color("6")  // ANSI cyan
	colorGreen   = lipgloss.Color("2")  // ANSI green
	colorRed     = lipgloss.Color("1")  // ANSI red
	colorMagenta = lipgloss.Color("5")  // ANSI magenta
	colorYellow  = lipgloss.Color("3")  // ANSI yellow
	colorDim     = lipgloss.Color("8")  // ANSI bright black (dim)
)

// Header styles.
var (
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleBoldCyan = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	styleDim      = lipgloss.NewStyle().Faint(true)
)

// Status icon styles.
var (
	styleSuccess = lipgloss.NewStyle().Foreground(colorGreen)
	styleError   = lipgloss.NewStyle().Foreground(colorRed)
	styleWarn    = lipgloss.NewStyle().Foreground(colorYellow)
	styleInfo    = lipgloss.NewStyle().Foreground(colorCyan)
)

// Chat area styles — matching Codex's user_message_style (subtle bg tint).
var (
	styleUserMsg = lipgloss.NewStyle().
			PaddingLeft(1).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(colorCyan)

	styleAIMsg = lipgloss.NewStyle().PaddingLeft(2)
)

// Footer / key-hint bar — dim text with cyan key labels.
var (
	styleFooter = lipgloss.NewStyle().Faint(true)
	styleKey    = lipgloss.NewStyle().Faint(true).Foreground(colorCyan)
)

// Input composer border.
var styleComposerBorder = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(colorCyan).
	Padding(0, 1)

// Stage progress styles.
var (
	styleStageActive = lipgloss.NewStyle().Foreground(colorCyan)
	styleStageDone   = lipgloss.NewStyle().Foreground(colorGreen)
	styleStageFail   = lipgloss.NewStyle().Foreground(colorRed)
	styleStageTime   = lipgloss.NewStyle().Faint(true)
)
