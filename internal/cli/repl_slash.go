package cli

import "strings"

type SlashCommand int

const (
	SlashNone SlashCommand = iota
	SlashUnknown
	SlashShip
	SlashShow
	SlashClear
	SlashHelp
	SlashExit
)

// ParseSlash returns SlashNone if the input is not a slash command,
// SlashUnknown if it starts with "/" but is not recognised, and the
// matching SlashCommand otherwise.
func ParseSlash(s string) SlashCommand {
	s = strings.TrimSpace(s)
	if s == "" || !strings.HasPrefix(s, "/") {
		return SlashNone
	}
	switch strings.ToLower(s) {
	case "/ship":
		return SlashShip
	case "/show":
		return SlashShow
	case "/clear":
		return SlashClear
	case "/help":
		return SlashHelp
	case "/exit", "/quit":
		return SlashExit
	default:
		return SlashUnknown
	}
}
