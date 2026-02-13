package openclaw

import (
	"strings"
	"testing"
)

func TestExtractCompleteJSONObjects(t *testing.T) {
	raw := "[diag] preface\n{\"payloads\":[{\"text\":\"Hello\"}]}\n{\"payloads\":[{\"text\":\"World\"}]"
	objects, remainder := extractCompleteJSONObjects(raw)
	if len(objects) != 1 {
		t.Fatalf("objects len = %d, want 1", len(objects))
	}
	if !strings.Contains(objects[0], "\"Hello\"") {
		t.Fatalf("objects[0] = %q, want Hello payload", objects[0])
	}
	if !strings.Contains(remainder, "\"World\"") {
		t.Fatalf("remainder = %q, want trailing incomplete world object", remainder)
	}
}

func TestUnseenSuffix(t *testing.T) {
	if got := unseenSuffix("hello", "hello world"); got != " world" {
		t.Fatalf("unseenSuffix() = %q, want %q", got, " world")
	}
	if got := unseenSuffix("hello world", "hello"); got != "" {
		t.Fatalf("unseenSuffix() = %q, want empty", got)
	}
	if got := unseenSuffix("alpha beta", "beta gamma"); got != " gamma" {
		t.Fatalf("unseenSuffix() overlap = %q, want %q", got, " gamma")
	}
}

func TestCLIStreamCollectorEmitsIncrementalAndDedupesFinal(t *testing.T) {
	c := newCLIStreamCollector(24)

	// First partial response appears without punctuation; should stay buffered.
	part1 := `{"payloads":[{"text":"Hello"}]}`
	if got := c.ConsumeChunk([]byte(part1)); len(got) != 0 {
		t.Fatalf("first deltas len = %d, want 0", len(got))
	}

	// Second object extends prior text and includes punctuation, so we should emit.
	part2 := `{"payloads":[{"text":"Hello world. More detail"}]}`
	got := c.ConsumeChunk([]byte(part2))
	if len(got) != 1 || got[0] != "Hello world." {
		t.Fatalf("deltas = %#v, want [\"Hello world.\"]", got)
	}

	// Final text repeats full content and adds an ending punctuation.
	final := "Hello world. More detail."
	remaining := c.Finalize(final)
	if len(remaining) != 1 || remaining[0] != " More detail." {
		t.Fatalf("remaining = %#v, want [\" More detail.\"]", remaining)
	}
}

func TestCLIStreamCollectorHandlesSplitObjectsAcrossChunks(t *testing.T) {
	c := newCLIStreamCollector(12)
	chunk1 := `{"payloads":[{"text":"Hello`
	chunk2 := ` world."}]}`

	if got := c.ConsumeChunk([]byte(chunk1)); len(got) != 0 {
		t.Fatalf("chunk1 deltas = %#v, want none", got)
	}
	got := c.ConsumeChunk([]byte(chunk2))
	if len(got) != 1 || got[0] != "Hello world." {
		t.Fatalf("chunk2 deltas = %#v, want [\"Hello world.\"]", got)
	}
}

func TestCLIStreamCollectorJoinedDeltasMatchFinalText(t *testing.T) {
	c := newCLIStreamCollector(12)
	var got strings.Builder

	chunk1 := `{"payloads":[{"text":"alpha beta gamma delta epsilon"}]}`
	for _, delta := range c.ConsumeChunk([]byte(chunk1)) {
		got.WriteString(delta)
	}

	chunk2 := `{"payloads":[{"text":"alpha beta gamma delta epsilon zeta eta theta"}]}`
	for _, delta := range c.ConsumeChunk([]byte(chunk2)) {
		got.WriteString(delta)
	}

	final := "alpha beta gamma delta epsilon zeta eta theta iota"
	for _, delta := range c.Finalize(final) {
		got.WriteString(delta)
	}

	if got.String() != final {
		t.Fatalf("joined deltas = %q, want %q", got.String(), final)
	}
}

func TestNormalizeStreamMinCharsDefaultsTo16(t *testing.T) {
	if got := normalizeStreamMinChars(0); got != 16 {
		t.Fatalf("normalizeStreamMinChars(0) = %d, want 16", got)
	}
}

func TestCLIStreamCollectorFirstMinTunedForLatency(t *testing.T) {
	c := newCLIStreamCollector(16)
	if c.firstMin != 6 {
		t.Fatalf("firstMin = %d, want 6", c.firstMin)
	}
}

func TestNextCLIStreamSegmentFlushesSoonerWithoutPunctuation(t *testing.T) {
	input := "alpha beta gamma delta zeta"
	segment, rest, ok := nextCLIStreamSegment(input, 16, false)
	if !ok {
		t.Fatalf("nextCLIStreamSegment() = no segment, want early flush")
	}
	if strings.TrimSpace(segment) == "" {
		t.Fatalf("segment = %q, want non-empty", segment)
	}
	if segment+rest != input {
		t.Fatalf("segment+rest mismatch: %q + %q != %q", segment, rest, input)
	}
}
