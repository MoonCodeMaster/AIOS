package cli

import "testing"

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		in   string
		want SlashCommand
	}{
		{"/ship", SlashShip},
		{"/show", SlashShow},
		{"/clear", SlashClear},
		{"/help", SlashHelp},
		{"/exit", SlashExit},
		{"/quit", SlashExit},
		{"/SHIP", SlashShip},
		{"  /ship  ", SlashShip},
		{"hello", SlashNone},
		{"/unknown", SlashUnknown},
		{"", SlashNone},
	}
	for _, tt := range tests {
		got := ParseSlash(tt.in)
		if got != tt.want {
			t.Errorf("ParseSlash(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
