package openclaw

import (
	"encoding/json"
	"strings"
)

type cliStreamCollector struct {
	minChars int
	firstMin int

	rawBuffer string
	pending   string
	emitted   string
}

func newCLIStreamCollector(minChars int) *cliStreamCollector {
	minChars = normalizeStreamMinChars(minChars)
	firstMin := minChars / 3
	if firstMin < 8 {
		firstMin = 8
	}
	if firstMin > minChars {
		firstMin = minChars
	}
	return &cliStreamCollector{
		minChars: minChars,
		firstMin: firstMin,
	}
}

func (c *cliStreamCollector) ConsumeChunk(chunk []byte) []string {
	if len(chunk) == 0 {
		return nil
	}
	c.rawBuffer += string(chunk)
	objects, remainder := extractCompleteJSONObjects(c.rawBuffer)
	c.rawBuffer = remainder

	var out []string
	for _, objRaw := range objects {
		var obj map[string]any
		if err := json.Unmarshal([]byte(objRaw), &obj); err != nil {
			continue
		}
		text := extractCLIText(obj)
		if strings.TrimSpace(text) == "" {
			continue
		}
		if unseen := unseenSuffix(c.emitted+c.pending, text); unseen != "" {
			c.pending += unseen
			out = append(out, c.flush(false)...)
		}
	}
	return out
}

func (c *cliStreamCollector) Finalize(finalText string) []string {
	finalText = strings.TrimSpace(finalText)
	if finalText != "" {
		if unseen := unseenSuffix(c.emitted+c.pending, finalText); unseen != "" {
			c.pending += unseen
		}
	}
	return c.flush(true)
}

func (c *cliStreamCollector) flush(force bool) []string {
	var out []string
	for {
		threshold := c.minChars
		if c.emitted == "" {
			threshold = c.firstMin
		}

		segment, rest, ok := nextCLIStreamSegment(c.pending, threshold, force)
		if !ok {
			break
		}
		c.pending = rest
		if c.emitted == "" && len(out) == 0 {
			segment = strings.TrimLeft(segment, " \t\r\n")
		}
		if strings.TrimSpace(segment) == "" {
			continue
		}
		out = append(out, segment)
		c.emitted += segment
	}
	return out
}

func nextCLIStreamSegment(input string, minChars int, force bool) (segment, rest string, ok bool) {
	if input == "" {
		return "", "", false
	}
	if force {
		return input, "", true
	}

	if idx := boundaryAfterMin(input, minChars); idx >= 0 {
		seg := input[:idx+1]
		rest := input[idx+1:]
		return seg, rest, true
	}

	// If we already buffered enough text without punctuation, flush a short chunk
	// to keep first-text latency and TTS responsiveness low.
	if len(input) >= minChars*2 {
		cut := whitespaceCut(input, minChars)
		seg := input[:cut]
		rest := input[cut:]
		return seg, rest, true
	}
	return "", input, false
}

func boundaryAfterMin(input string, minChars int) int {
	if minChars < 1 {
		minChars = 1
	}
	for i := minChars - 1; i < len(input); i++ {
		switch input[i] {
		case '.', '!', '?', '\n':
			return i
		}
	}
	return -1
}

func whitespaceCut(input string, minChars int) int {
	if minChars < 1 {
		minChars = 1
	}
	if len(input) <= minChars {
		return len(input)
	}
	limit := minChars + 20
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

func extractCLIText(obj map[string]any) string {
	payloads := payloadArrayFrom(obj)
	if len(payloads) == 0 {
		return pickStringField(obj, "text", "delta", "output", "message")
	}
	parts := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		text, _ := payload["text"].(string)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractCompleteJSONObjects(raw string) (objects []string, remainder string) {
	remainder = raw
	for {
		start := strings.IndexByte(remainder, '{')
		if start < 0 {
			if len(remainder) > 8192 {
				remainder = remainder[len(remainder)-8192:]
			}
			return objects, remainder
		}
		if start > 0 {
			remainder = remainder[start:]
		}

		end := jsonObjectEndIndex(remainder)
		if end < 0 {
			if len(remainder) > 4*1024*1024 {
				remainder = remainder[len(remainder)-(2*1024*1024):]
			}
			return objects, remainder
		}

		objects = append(objects, remainder[:end+1])
		remainder = remainder[end+1:]
	}
}

func jsonObjectEndIndex(raw string) int {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func unseenSuffix(already, next string) string {
	if next == "" {
		return ""
	}
	if already == "" {
		return next
	}
	if strings.HasPrefix(next, already) {
		return next[len(already):]
	}
	if strings.HasPrefix(already, next) {
		return ""
	}
	overlap := suffixPrefixOverlap(already, next)
	if overlap > 0 {
		return next[overlap:]
	}
	return next
}

func suffixPrefixOverlap(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for n := max; n > 0; n-- {
		if a[len(a)-n:] == b[:n] {
			return n
		}
	}
	return 0
}
