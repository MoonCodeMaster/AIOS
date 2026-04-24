package streamui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Stream is a thread-safe writer for "watch-only" run output.
// It flushes each line with a timestamp prefix.
type Stream struct {
	mu  sync.Mutex
	out io.Writer
}

func New(w io.Writer) *Stream { return &Stream{out: w} }

func (s *Stream) Event(category, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := time.Now().UTC().Format("15:04:05")
	fmt.Fprintf(s.out, "[%s] %-9s %s\n", ts, category, msg)
}
