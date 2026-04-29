package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// stageTicker is a thread-safe live status renderer for the dual-AI pipeline.
type stageTicker struct {
	out   io.Writer
	isTTY bool

	mu       sync.Mutex
	active   map[string]time.Time
	order    []string
	dirty    bool
	frame    int
}

func newStageTicker(out io.Writer) *stageTicker {
	return &stageTicker{
		out:    out,
		isTTY:  isTerminal(out),
		active: make(map[string]time.Time),
	}
}

func (t *stageTicker) Start(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[name] = time.Now()
	t.order = append(t.order, name)
	if t.isTTY {
		t.draw()
		return
	}
	cDim.Fprintf(t.out, "  · %s …\n", name)
}

func (t *stageTicker) Progress(_ string, _ time.Duration) {
	if !t.isTTY {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.frame++
	t.draw()
}

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
		fmt.Fprintf(t.out, "  %s %s %s %s\n",
			cRed.Sprint("✗"),
			name,
			cRed.Sprintf("failed in %s:", rounded),
			err)
	} else {
		fmt.Fprintf(t.out, "  %s %s %s\n",
			cGreen.Sprint("✓"),
			name,
			cDim.Sprintf("(%s)", rounded))
	}
	if t.isTTY && len(t.active) > 0 {
		t.draw()
	}
}

func (t *stageTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.isTTY {
		t.clear()
	}
}

func (t *stageTicker) draw() {
	if len(t.order) == 0 {
		t.clear()
		return
	}
	spinner := cCyan.Sprint(spinnerFrames[t.frame%len(spinnerFrames)])
	var parts []string
	for _, n := range t.order {
		e := time.Since(t.active[n]).Round(time.Second)
		parts = append(parts, fmt.Sprintf("%s %s", n, cDim.Sprint(formatElapsed(e))))
	}
	fmt.Fprintf(t.out, "\r\033[K  %s %s", spinner, strings.Join(parts, cDim.Sprint(" · ")))
	t.dirty = true
}

func (t *stageTicker) clear() {
	if t.dirty {
		fmt.Fprint(t.out, "\r\033[K")
		t.dirty = false
	}
}

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
