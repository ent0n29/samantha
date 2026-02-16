package voice

import (
	"strings"
	"testing"
)

func TestSplitTTSReadySegmentsPrefersSentenceBoundary(t *testing.T) {
	text := "I can help you with that and keep this natural while we think through it together. Then we can continue."
	ready, rest := splitTTSReadySegments(text, 42, 220)
	if len(ready) == 0 {
		t.Fatalf("expected at least one ready segment")
	}
	if !strings.HasSuffix(ready[0], ".") {
		t.Fatalf("first segment = %q, want sentence boundary", ready[0])
	}
	if len(ready[0]) < 42 {
		t.Fatalf("first segment too short: len=%d", len(ready[0]))
	}
	_ = rest
}

func TestSplitTTSReadySegmentsAvoidsPrematureFragment(t *testing.T) {
	// Length is >= minChars, but still below the fallback threshold (min + 40),
	// so we should keep buffering instead of emitting a tiny robotic chunk.
	text := strings.Repeat("word ", 14)
	ready, rest := splitTTSReadySegments(text, 42, 220)
	if len(ready) != 0 {
		t.Fatalf("ready segments = %d, want 0 to avoid robotic fragmentation", len(ready))
	}
	if strings.TrimSpace(rest) != strings.TrimSpace(text) {
		t.Fatalf("unexpected remainder: got %q want %q", strings.TrimSpace(rest), strings.TrimSpace(text))
	}
}

func TestSplitTTSReadySegmentsLongTextRespectsBounds(t *testing.T) {
	text := strings.Repeat("steady speech flow without punctuation ", 12)
	minChars := 42
	maxChars := 120
	ready, _ := splitTTSReadySegments(text, minChars, maxChars)
	if len(ready) == 0 {
		t.Fatalf("expected at least one segment")
	}
	for i, seg := range ready {
		if len(seg) < minChars {
			t.Fatalf("segment %d too short: len=%d min=%d", i, len(seg), minChars)
		}
		if len(seg) > maxChars {
			t.Fatalf("segment %d too long: len=%d max=%d", i, len(seg), maxChars)
		}
	}
}

func TestLocalTTSSegmentBounds(t *testing.T) {
	min, max := localTTSSegmentBounds(false, 1.0)
	if min != 24 || max != 220 {
		t.Fatalf("first chunk bounds = (%d,%d), want (24,220)", min, max)
	}

	min, max = localTTSSegmentBounds(true, 1.0)
	if min != 72 || max != 320 {
		t.Fatalf("steady chunk bounds = (%d,%d), want (72,320)", min, max)
	}

	minFast, maxFast := localTTSSegmentBounds(true, 1.08)
	if minFast >= min || maxFast >= max {
		t.Fatalf("fast bounds = (%d,%d), want smaller than steady (%d,%d)", minFast, maxFast, min, max)
	}

	minSlow, maxSlow := localTTSSegmentBounds(true, 0.88)
	if minSlow <= min || maxSlow <= max {
		t.Fatalf("slow bounds = (%d,%d), want larger than steady (%d,%d)", minSlow, maxSlow, min, max)
	}
}
