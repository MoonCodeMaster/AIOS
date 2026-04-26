package architect

import (
	"strings"
)

const (
	beginMarker = "===BLUEPRINT==="
	endMarker   = "===END==="
)

// ParseBlueprints extracts every well-formed Blueprint block from raw model
// output. Malformed blocks (missing required fields) are silently dropped —
// callers that need a specific count check len() themselves.
func ParseBlueprints(raw string) []Blueprint {
	var out []Blueprint
	for _, block := range splitBlocks(raw) {
		bp := parseBlock(block)
		if !bp.Valid() {
			continue
		}
		out = append(out, bp)
	}
	return out
}

// splitBlocks returns every chunk of text between a `===BLUEPRINT===` line
// and the next `===END===` line. Lines outside any pair are ignored, which
// makes the parser tolerant of preamble/postamble the model occasionally
// emits despite being told not to.
func splitBlocks(raw string) []string {
	var blocks []string
	var current []string
	inside := false
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		switch trim {
		case beginMarker:
			inside = true
			current = current[:0]
		case endMarker:
			if inside {
				blocks = append(blocks, strings.Join(current, "\n"))
				current = current[:0]
			}
			inside = false
		default:
			if inside {
				current = append(current, line)
			}
		}
	}
	return blocks
}

// parseBlock splits one block into headed sections. The header lines
// (title/tagline/stance) come before the first `## ` heading; everything
// after a `## X` line until the next `## ` line is X's section content.
func parseBlock(block string) Blueprint {
	var bp Blueprint
	lines := strings.Split(block, "\n")

	// Phase 1 — header key:value lines until the first `## ` heading.
	i := 0
	for ; i < len(lines); i++ {
		ln := lines[i]
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "## ") {
			break
		}
		if k, v, ok := splitKV(trim); ok {
			switch strings.ToLower(k) {
			case "title":
				bp.Title = v
			case "tagline":
				bp.Tagline = v
			case "stance":
				bp.Stance = v
			}
		}
	}

	// Phase 2 — section blocks. Each section runs until the next `## `
	// or end of block.
	currentHead := ""
	var currentBody []string
	flush := func() {
		body := strings.TrimRight(strings.Join(currentBody, "\n"), "\n")
		switch strings.ToLower(currentHead) {
		case "mind map":
			bp.MindMap = body
		case "architecture sketch":
			bp.Sketch = body
		case "data flow":
			bp.DataFlow = body
		case "tradeoffs":
			bp.Tradeoff = body
		case "roadmap":
			bp.Roadmap = body
		case "risks":
			bp.Risks = body
		}
		currentBody = currentBody[:0]
	}
	for ; i < len(lines); i++ {
		ln := lines[i]
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "## ") {
			if currentHead != "" {
				flush()
			}
			currentHead = strings.TrimSpace(strings.TrimPrefix(trim, "##"))
			continue
		}
		currentBody = append(currentBody, ln)
	}
	if currentHead != "" {
		flush()
	}
	return bp
}

func splitKV(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}
