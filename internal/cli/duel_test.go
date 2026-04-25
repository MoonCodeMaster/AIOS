package cli

import "testing"

func TestParseVerdict_Winner(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "winner A",
			raw: `===VERDICT===
winner: A
correctness: a is correct, b broke it
minimality: a smaller
clarity: a clearer
reason: a is better in every axis
===END===`,
			want: "A",
		},
		{
			name: "winner B (case insensitive)",
			raw: `===VERDICT===
winner: b
reason: b wins
===END===`,
			want: "B",
		},
		{
			name: "tie",
			raw: `===VERDICT===
winner: tie
reason: indistinguishable
===END===`,
			want: "tie",
		},
		{
			name: "missing block defaults to tie",
			raw:  "no markers here, just prose",
			want: "tie",
		},
		{
			name: "garbage winner value defaults to tie",
			raw: `===VERDICT===
winner: probably A i think
reason: hard to say
===END===`,
			want: "tie",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseVerdict(c.raw)
			if got.Winner != c.want {
				t.Errorf("Winner = %q, want %q", got.Winner, c.want)
			}
		})
	}
}

func TestParseVerdict_MultilineFields(t *testing.T) {
	raw := `===VERDICT===
winner: A
correctness: A correctly handled the empty-input case
  while B errored on empty input.
  Three lines of analysis here.
minimality: A's diff is 5 lines; B's is 23.
reason: clear correctness gap
===END===`
	got := parseVerdict(raw)
	if got.Winner != "A" {
		t.Errorf("Winner = %q", got.Winner)
	}
	if got.Correctness == "" || got.Minimality == "" || got.Reason == "" {
		t.Errorf("free-text fields not populated: %+v", got)
	}
	// Multi-line correctness should preserve continuation lines.
	if !contains(got.Correctness, "Three lines of analysis here.") {
		t.Errorf("Correctness lost continuation lines: %q", got.Correctness)
	}
}

func TestWinnerLabel(t *testing.T) {
	cases := map[string]string{
		"A":       "claude wins",
		"B":       "codex wins",
		"tie":     "tie",
		"unknown": "tie",
	}
	for in, want := range cases {
		got := winnerLabel(in, "claude", "codex")
		if got != want {
			t.Errorf("winnerLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
