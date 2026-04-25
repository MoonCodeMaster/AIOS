package architect

import (
	"strings"
)

// Render writes a Blueprint back to the canonical block format the parser
// reads. Used both for user-facing display and for round-trip tests.
func Render(b Blueprint) string {
	var s strings.Builder
	s.WriteString(beginMarker)
	s.WriteByte('\n')
	s.WriteString("title: " + b.Title + "\n")
	s.WriteString("tagline: " + b.Tagline + "\n")
	s.WriteString("stance: " + b.Stance + "\n")
	writeSection(&s, "Mind map", b.MindMap)
	writeSection(&s, "Architecture sketch", b.Sketch)
	writeSection(&s, "Data flow", b.DataFlow)
	writeSection(&s, "Tradeoffs", b.Tradeoff)
	writeSection(&s, "Roadmap", b.Roadmap)
	writeSection(&s, "Risks", b.Risks)
	s.WriteString(endMarker)
	s.WriteByte('\n')
	return s.String()
}

// RenderForUser is the human-facing variant: the same content but with the
// machine markers stripped and a numbered banner so the user can pick by
// number. Stance is shown right under the title because it is the field
// that distinguishes the three finalists.
func RenderForUser(n int, b Blueprint) string {
	var s strings.Builder
	banner := strings.Repeat("─", 78)
	s.WriteString(banner + "\n")
	s.WriteString("BLUEPRINT " + itoa(n) + " — " + b.Title + "\n")
	s.WriteString("stance:  " + b.Stance + "\n")
	if b.Tagline != "" {
		s.WriteString("tagline: " + b.Tagline + "\n")
	}
	s.WriteString(banner + "\n")
	if b.MindMap != "" {
		s.WriteString("\n## Mind map\n" + b.MindMap + "\n")
	}
	if b.Sketch != "" {
		s.WriteString("\n## Architecture sketch\n" + b.Sketch + "\n")
	}
	if b.DataFlow != "" {
		s.WriteString("\n## Data flow\n" + b.DataFlow + "\n")
	}
	if b.Tradeoff != "" {
		s.WriteString("\n## Tradeoffs\n" + b.Tradeoff + "\n")
	}
	if b.Roadmap != "" {
		s.WriteString("\n## Roadmap\n" + b.Roadmap + "\n")
	}
	if b.Risks != "" {
		s.WriteString("\n## Risks\n" + b.Risks + "\n")
	}
	return s.String()
}

func writeSection(s *strings.Builder, head, body string) {
	if body == "" {
		return
	}
	s.WriteString("\n## " + head + "\n")
	s.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		s.WriteByte('\n')
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
