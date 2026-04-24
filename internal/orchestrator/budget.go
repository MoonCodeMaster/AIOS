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

// Reason returns "" if nothing exceeded, else a short code. Kept for
// backward compatibility; new code should prefer budgetBlock.
func (b *Budget) ExceededReason() string {
	switch {
	case b.RoundsExceeded():
		return string(CodeMaxRoundsExceeded)
	case b.TokensExceeded():
		return string(CodeMaxTokensExceeded)
	case b.WallExceeded():
		return string(CodeMaxWallExceeded)
	}
	return ""
}

// budgetBlock returns a structured BlockReason when any budget is exceeded,
// or nil when budgets are still within limits. Used by orchestrator.Run at
// both the top-of-loop budget check and the mid-round post-coder check.
func budgetBlock(b *Budget) *BlockReason {
	switch {
	case b.RoundsExceeded():
		return NewBlock(CodeMaxRoundsExceeded, "")
	case b.TokensExceeded():
		return NewBlock(CodeMaxTokensExceeded, "")
	case b.WallExceeded():
		return NewBlock(CodeMaxWallExceeded, "")
	}
	return nil
}
