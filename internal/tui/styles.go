package tui

import "github.com/charmbracelet/lipgloss"

// Styles matching the Codex CLI visual language.
var (
	// Brand / header.
	styleBoldCyan = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleModel    = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	// Chat.
	styleUserLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	styleUserMsg   = lipgloss.NewStyle()
	styleAIMsg     = lipgloss.NewStyle()

	// Status.
	styleInfo    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	// Stages.
	styleStageActive = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleStageDone   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleStageFail   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleStageTime   = lipgloss.NewStyle().Faint(true)

	// Composer.
	stylePrompt        = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleComposerBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8")).
				Padding(0, 1)

	// Footer.
	styleKey    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleFooter = lipgloss.NewStyle()

	// Slash commands.
	styleCmd      = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)
