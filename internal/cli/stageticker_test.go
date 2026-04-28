package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// In non-TTY mode (a bytes.Buffer is not a *os.File), the ticker must emit
// one `· name …` line on Start and one `✓/✗ name (Xms)` line on End — and
// nothing during Progress. This preserves the test/CI log story.
func TestStageTicker_NonTTYStartEndOnly(t *testing.T) {
	var buf bytes.Buffer
	tk := newStageTicker(&buf)
	if tk.isTTY {
		t.Fatal("bytes.Buffer should not be detected as a TTY")
	}

	tk.Start("draft-claude")
	tk.Progress("draft-claude", 1*time.Second)
	tk.Progress("draft-claude", 2*time.Second)
	tk.End("draft-claude", nil)

	out := buf.String()
	if !strings.Contains(out, "· draft-claude …") {
		t.Errorf("missing start line; got %q", out)
	}
	if !strings.Contains(out, "✓ draft-claude") {
		t.Errorf("missing end line; got %q", out)
	}
	if strings.Contains(out, "↻") {
		t.Errorf("non-TTY mode must not emit live status (↻); got %q", out)
	}
}

// End records the failed engine message so the operator sees the actual
// reason the stage died (auth, timeout, etc.) rather than just a ✗ marker.
func TestStageTicker_EndOnErrorIncludesMessage(t *testing.T) {
	var buf bytes.Buffer
	tk := newStageTicker(&buf)

	tk.Start("draft-codex")
	tk.End("draft-codex", errors.New("codex timed out after 600s"))

	out := buf.String()
	if !strings.Contains(out, "✗ draft-codex failed") {
		t.Errorf("missing failure marker; got %q", out)
	}
	if !strings.Contains(out, "codex timed out after 600s") {
		t.Errorf("missing engine error detail; got %q", out)
	}
}

// formatElapsed must produce compact strings without sub-second noise so the
// live status line stays readable in narrow terminals.
func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m00s"},
		{72 * time.Second, "1m12s"},
		{3 * time.Minute, "3m00s"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.in); got != c.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}
