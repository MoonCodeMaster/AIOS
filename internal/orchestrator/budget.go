package orchestrator

import "time"

type Budget struct {
	maxRounds int
	maxTokens int
	maxWall   time.Duration
	startedAt time.Time
	rounds    int
	tokens    int
}

func NewBudget(maxRounds, maxTokens int, maxWall time.Duration) *Budget {
	return &Budget{
		maxRounds: maxRounds,
		maxTokens: maxTokens,
		maxWall:   maxWall,
		startedAt: time.Now(),
	}
}

func (b *Budget) BumpRound()           { b.rounds++ }
func (b *Budget) AddTokens(n int)      { b.tokens += n }
func (b *Budget) Rounds() int          { return b.rounds }
func (b *Budget) Tokens() int          { return b.tokens }
func (b *Budget) RoundsExceeded() bool { return b.rounds >= b.maxRounds }
func (b *Budget) TokensExceeded() bool { return b.tokens > b.maxTokens }
func (b *Budget) WallExceeded() bool   { return time.Since(b.startedAt) > b.maxWall }

// Reason returns "" if nothing exceeded, else a short code.
func (b *Budget) ExceededReason() string {
	switch {
	case b.RoundsExceeded():
		return "max_rounds_exceeded"
	case b.TokensExceeded():
		return "max_tokens_exceeded"
	case b.WallExceeded():
		return "max_wall_exceeded"
	}
	return ""
}
