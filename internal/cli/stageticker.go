package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// stageTicker is a thread-safe live status renderer for the dual-AI pipeline.
//
// In a TTY it draws a single redrawing line listing every active stage with
// elapsed time (e.g. `↻ draft-claude 23s · draft-codex 19s`). When a stage
// completes, the status line is cleared, a permanent ✓/✗ summary line is
// printed, and the status redraws if other stages are still active.
//
// In a non-TTY (tests, pipes, CI logs) it falls back to one line per
// start/end so output stays readable without ANSI escapes.
type stageTicker struct {
	out   io.Writer
	isTTY bool

	mu       sync.Mutex
	active   map[string]time.Time // stage name -> start time
	order    []string             // insertion order for stable rendering
	dirty    bool                 // true if the status line was last drawn (needs clear before next plain print)
}

func newStageTicker(out io.Writer) *stageTicker {
	return &stageTicker{
		out:    out,
		isTTY:  isTerminal(out),
		active: make(map[string]time.Time),
	}
}

// Start records that a stage is now in flight.
func (t *stageTicker) Start(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[name] = time.Now()
	t.order = append(t.order, name)
	if t.isTTY {
		t.draw()
		return
	}
	fmt.Fprintf(t.out, "  · %s …\n", name)
}

// Progress is invoked periodically by the pipeline while a stage runs.
// In TTY mode it triggers a redraw. In non-TTY mode it's a no-op — we don't
// want a once-per-second log spam in CI.
func (t *stageTicker) Progress(_ string, _ time.Duration) {
	if !t.isTTY {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.draw()
}

// End records that a stage finished and prints a permanent summary line.
func (t *stageTicker) End(name string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	start, ok := t.active[name]
	elapsed := time.Duration(0)
	if ok {
		elapsed = time.Since(start)
		delete(t.active, name)
		t.order = removeOrdered(t.order, name)
	}
	if t.isTTY {
		t.clear()
	}
	rounded := elapsed.Round(time.Millisecond)
	if err != nil {
		fmt.Fprintf(t.out, "  ✗ %s failed in %s: %v\n", name, rounded, err)
	} else {
		fmt.Fprintf(t.out, "  ✓ %s (%s)\n", name, rounded)
	}
	if t.isTTY && len(t.active) > 0 {
		t.draw()
	}
}

// Stop clears any in-flight status line. Call before printing trailing
// content (warnings, summaries) so the live line doesn't bleed into them.
func (t *stageTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.isTTY {
		t.clear()
	}
}

// draw renders the current active set as a single line. Caller must hold mu.
func (t *stageTicker) draw() {
	if len(t.order) == 0 {
		t.clear()
		return
	}
	var parts []string
	for _, n := range t.order {
		e := time.Since(t.active[n]).Round(time.Second)
		parts = append(parts, fmt.Sprintf("%s %s", n, formatElapsed(e)))
	}
	// \r returns to column 0; \033[K clears to end-of-line so a shorter
	// status line cleanly overwrites a longer previous one.
	fmt.Fprintf(t.out, "\r\033[K  ↻ %s", strings.Join(parts, " · "))
	t.dirty = true
}

// clear erases the live status line. Caller must hold mu.
func (t *stageTicker) clear() {
	if t.dirty {
		fmt.Fprint(t.out, "\r\033[K")
		t.dirty = false
	}
}

// formatElapsed renders a duration as e.g. "23s" or "1m12s".
// time.Duration.String() returns "1m12.000s" with the trailing zeros, which
// is noisy for a status line — we strip the sub-second component.
func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

func removeOrdered(xs []string, x string) []string {
	for i, v := range xs {
		if v == x {
			return append(xs[:i], xs[i+1:]...)
		}
	}
	return xs
}

// isTerminal reports whether w is an interactive terminal. A non-*os.File
// writer (bytes.Buffer in tests, pipes) returns false; an *os.File writer
// returns true only when its mode has the character-device bit set.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
