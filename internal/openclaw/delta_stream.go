package openclaw

import "strings"

// deltaStreamCollector coalesces small streamed deltas into phrase-ish chunks so
// TTS and the UI don't receive a firehose of token-sized fragments.
type deltaStreamCollector struct {
	minChars int
	firstMin int

	pending string
	emitted string
}

func newDeltaStreamCollector(minChars int) *deltaStreamCollector {
	minChars = normalizeStreamMinChars(minChars)
	// First emission should be as soon as we have "something" so the UI/TTS can start
	// feeling responsive; later chunks can be larger for smoother flow.
	firstMin := minChars / 4
	if firstMin < 2 {
		firstMin = 2
	}
	if firstMin > minChars {
		firstMin = minChars
	}
	return &deltaStreamCollector{
		minChars: minChars,
		firstMin: firstMin,
	}
}

func (c *deltaStreamCollector) Consume(delta string) []string {
	if delta == "" {
		return nil
	}
	c.pending += delta
	return c.flush(false)
}

func (c *deltaStreamCollector) Finalize() []string {
	return c.flush(true)
}

func (c *deltaStreamCollector) flush(force bool) []string {
	var out []string
	for {
		threshold := c.minChars
		if c.emitted == "" {
			threshold = c.firstMin
		}

		segment, rest, ok := nextCLIStreamSegment(c.pending, threshold, c.emitted == "", force)
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
