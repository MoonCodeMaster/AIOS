package streamui

import (
	"bytes"
	"strings"
	"testing"
)

func TestStream_Event(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	s.Event("task", "start 001")
	s.Event("round", "1 coder")
	out := buf.String()
	if !strings.Contains(out, "start 001") || !strings.Contains(out, "1 coder") {
		t.Errorf("unexpected output: %s", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("expected 2 lines: %s", out)
	}
}
