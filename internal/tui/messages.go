package tui

import "time"

// Messages exchanged between the REPL wiring layer and the TUI.

type tickMsg time.Time

type stageStartMsg struct{ Name string }
type stageEndMsg struct {
	Name    string
	Elapsed time.Duration
	Err     error
}
type stageProgressMsg struct{ Name string }

type specDoneMsg struct {
	Final    string
	Lines    int
	Warnings []string
	Err      error
}

// Streaming messages for live response rendering.
type streamChunkMsg struct{ Text string }
type streamDoneMsg struct{}

// chatEntry represents one item in the chat history.
type chatEntry struct {
	Role    string // "user", "ai", "system"
	Content string // pre-rendered content
}
