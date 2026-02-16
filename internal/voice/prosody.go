package voice

import "strings"

type prosodyPlanner struct {
	buffer          string
	emittedAnyChunk bool
}

const (
	prosodyFirstChunkMin = 24
	prosodyNextChunkMin  = 42
	prosodyCutWindow     = 44
)

func newProsodyPlanner() *prosodyPlanner {
	return &prosodyPlanner{}
}

func (p *prosodyPlanner) Push(delta string) []string {
	if strings.TrimSpace(delta) == "" {
		return nil
	}
	p.buffer += delta
	return p.flush(false)
}

func (p *prosodyPlanner) Finalize() []string {
	return p.flush(true)
}

func (p *prosodyPlanner) flush(force bool) []string {
	var out []string
	for {
		minChars := prosodyNextChunkMin
		if !p.emittedAnyChunk {
			minChars = prosodyFirstChunkMin
		}
		segment, rest, ok := nextProsodySegment(p.buffer, minChars, force)
		if !ok {
			break
		}
		p.buffer = rest
		segment = normalizeProsodySegment(segment)
		if segment == "" {
			continue
		}
		p.emittedAnyChunk = true
		out = append(out, segment)
	}
	return out
}

func nextProsodySegment(input string, minChars int, force bool) (segment, rest string, ok bool) {
	if input == "" {
		return "", "", false
	}
	if force {
		return input, "", true
	}
	if len(input) < minChars {
		return "", input, false
	}

	if idx := commaBoundary(input, minChars); idx >= 0 {
		return input[:idx+1], input[idx+1:], true
	}
	if idx := punctuationBoundary(input, minChars); idx >= 0 {
		return input[:idx+1], input[idx+1:], true
	}

	cut := whitespaceBoundary(input, minChars, prosodyCutWindow)
	if cut <= 0 {
		return "", input, false
	}
	return input[:cut], input[cut:], true
}

func punctuationBoundary(input string, minChars int) int {
	for i := minChars - 1; i < len(input); i++ {
		switch input[i] {
		case '.', '!', '?', ';', ':', '\n':
			return i
		}
	}
	return -1
}

func commaBoundary(input string, minChars int) int {
	for i := minChars - 1; i < len(input); i++ {
		if input[i] == ',' {
			return i
		}
	}
	return -1
}

func whitespaceBoundary(input string, minChars int, window int) int {
	if len(input) <= minChars {
		return len(input)
	}
	limit := minChars + window
	if limit > len(input) {
		limit = len(input)
	}
	for i := minChars; i < limit; i++ {
		switch input[i] {
		case ' ', '\t', '\n', '\r':
			return i
		}
	}
	return minChars
}

func normalizeProsodySegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Fields(raw)
	return strings.TrimSpace(strings.Join(parts, " "))
}
