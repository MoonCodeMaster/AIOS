package tui

import "strings"

// SlashCommand defines a slash command available in the TUI.
type SlashCommand struct {
	Name        string
	Description string
	HasArgs     bool
}

// AllSlashCommands is the ordered list of available slash commands,
// matching the Codex CLI presentation order.
var AllSlashCommands = []SlashCommand{
	{Name: "model", Description: "Show current model configuration"},
	{Name: "show", Description: "Print current spec"},
	{Name: "ship", Description: "Hand the spec to autopilot"},
	{Name: "diff", Description: "Show git diff of current changes"},
	{Name: "status", Description: "Show session config and token usage"},
	{Name: "compact", Description: "Summarize conversation to save context"},
	{Name: "new", Description: "Start a new conversation"},
	{Name: "rename", Description: "Rename the current session", HasArgs: true},
	{Name: "clear", Description: "Clear conversation history"},
	{Name: "help", Description: "Show available commands"},
	{Name: "exit", Description: "Exit AIOS"},
}

// filterSlashCommands returns commands matching the given prefix (fuzzy).
func filterSlashCommands(filter string) []SlashCommand {
	if filter == "" {
		return AllSlashCommands
	}
	var matches []SlashCommand
	lower := strings.ToLower(filter)
	for _, sc := range AllSlashCommands {
		if strings.HasPrefix(sc.Name, lower) {
			matches = append(matches, sc)
		}
	}
	// Fuzzy fallback: substring match.
	if len(matches) == 0 {
		for _, sc := range AllSlashCommands {
			if strings.Contains(sc.Name, lower) {
				matches = append(matches, sc)
			}
		}
	}
	return matches
}
