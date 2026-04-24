package orchestrator

import (
	"testing"
	"time"
)

func TestBudget_RoundsExhaustion(t *testing.T) {
	b := NewBudget(3, 1000, time.Minute)
	if b.RoundsExceeded() {
		t.Error("fresh budget should not be exceeded")
	}
	for i := 0; i < 3; i++ {
		b.BumpRound()
	}
	if !b.RoundsExceeded() {
		t.Error("should be exceeded after 3 rounds with cap 3")
	}
}

func TestBudget_Tokens(t *testing.T) {
	b := NewBudget(10, 100, time.Minute)
	b.AddTokens(40)
	b.AddTokens(40)
	if b.TokensExceeded() {
		t.Error("not yet")
	}
	b.AddTokens(40)
	if !b.TokensExceeded() {
		t.Error("should be exceeded at 120 with cap 100")
	}
}

func TestBudget_WallClock(t *testing.T) {
	b := NewBudget(10, 100, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if !b.WallExceeded() {
		t.Error("wall clock should be exceeded")
	}
}
