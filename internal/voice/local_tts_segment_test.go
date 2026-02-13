package voice

import (
	"strings"
	"testing"
)

func TestSplitTTSReadySegmentsKeepsShortUnpunctuatedTail(t *testing.T) {
	ready, rest := splitTTSReadySegments("hello world", 12, 180)
	if len(ready) != 0 {
		t.Fatalf("ready len = %d, want 0", len(ready))
	}
	if rest != "hello world" {
		t.Fatalf("rest = %q, want %q", rest, "hello world")
	}
}

func TestSplitTTSReadySegmentsUsesSentenceBoundary(t *testing.T) {
	ready, rest := splitTTSReadySegments("Hello there. Next sentence", 12, 180)
	if len(ready) == 0 {
		t.Fatalf("ready len = 0, want >= 1")
	}
	if ready[0] != "Hello there." {
		t.Fatalf("first segment = %q, want %q", ready[0], "Hello there.")
	}
	if rest != "Next sentence" {
		t.Fatalf("rest = %q, want %q", rest, "Next sentence")
	}
}

func TestSplitTTSReadySegmentsFlushesWithoutPunctuation(t *testing.T) {
	input := "hello world this is a stream with no punctuation yet and it should start speaking soon"
	ready, rest := splitTTSReadySegments(input, 12, 180)
	if len(ready) == 0 {
		t.Fatalf("ready len = 0, want >= 1")
	}
	if len(ready[0]) < 12 {
		t.Fatalf("first segment len = %d, want >= 12", len(ready[0]))
	}
	if len(ready[0]) > 40 {
		t.Fatalf("first segment len = %d, want <= 40", len(ready[0]))
	}
	if strings.TrimSpace(rest) == "" {
		t.Fatalf("rest unexpectedly empty; want tail for continued streaming")
	}
}
