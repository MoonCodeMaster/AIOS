package tui

import "time"

// Bubbletea message types for the TUI event loop.

// stageStartMsg signals a pipeline stage has started.
type stageStartMsg struct{ Name string }

// stageEndMsg signals a pipeline stage completed.
type stageEndMsg struct {
	Name    string
	Elapsed time.Duration
	Err     error
}

// stageProgressMsg is a periodic tick for active stages.
type stageProgressMsg struct{}

// specDoneMsg signals the specgen pipeline finished.
type specDoneMsg struct {
	Final    string
	Lines    int
	Warnings []string
	Err      error
}

// tickMsg drives shimmer animation and elapsed-time updates.
type tickMsg time.Time

// chatEntry is one item in the scrollable chat history.
type chatEntry struct {
	Role    string // "user", "ai", "system"
	Content string // raw text or rendered markdown
}
