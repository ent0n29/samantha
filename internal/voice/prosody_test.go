package voice

import "testing"

func TestProsodyPlannerFlushesOnPunctuation(t *testing.T) {
	p := newProsodyPlanner()
	out := p.Push("We should ship this today. Then we can benchmark it.")
	if len(out) == 0 {
		t.Fatalf("Push() returned no chunks, want at least one")
	}
	if out[0] != "We should ship this today." {
		t.Fatalf("first chunk = %q, want %q", out[0], "We should ship this today.")
	}
}

func TestProsodyPlannerFinalizeFlushesRemainder(t *testing.T) {
	p := newProsodyPlanner()
	if got := p.Push("Short text"); len(got) != 0 {
		t.Fatalf("Push(short) chunks = %d, want 0", len(got))
	}
	final := p.Finalize()
	if len(final) != 1 {
		t.Fatalf("Finalize() chunks = %d, want 1", len(final))
	}
	if final[0] != "Short text" {
		t.Fatalf("Finalize() chunk = %q, want %q", final[0], "Short text")
	}
}

func TestProsodyPlannerNormalizesWhitespace(t *testing.T) {
	p := newProsodyPlanner()
	out := p.Push("We   should    ship this   today, and   then validate.")
	if len(out) == 0 {
		t.Fatalf("Push() returned no chunks")
	}
	if out[0] != "We should ship this today," {
		t.Fatalf("first chunk = %q, want %q", out[0], "We should ship this today,")
	}
}
