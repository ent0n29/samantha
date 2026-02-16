package voice

import (
	"testing"
	"time"
)

func TestBuildSemanticEndpointHintContinuation(t *testing.T) {
	hint, ok := buildSemanticEndpointHint("and then we can", 0.78, 1400*time.Millisecond)
	if !ok {
		t.Fatalf("buildSemanticEndpointHint() ok=false, want true")
	}
	if hint.Reason != "continuation" {
		t.Fatalf("Reason = %q, want %q", hint.Reason, "continuation")
	}
	if hint.Hold < 400*time.Millisecond {
		t.Fatalf("Hold = %s, want >= 400ms for continuation", hint.Hold)
	}
	if hint.ShouldCommit {
		t.Fatalf("ShouldCommit = true, want false")
	}
}

func TestBuildSemanticEndpointHintContinuationOpenTailWord(t *testing.T) {
	hint, ok := buildSemanticEndpointHint("we should connect this to", 0.81, 1500*time.Millisecond)
	if !ok {
		t.Fatalf("buildSemanticEndpointHint() ok=false, want true")
	}
	if hint.Reason != "continuation" {
		t.Fatalf("Reason = %q, want %q", hint.Reason, "continuation")
	}
	if hint.Hold < 400*time.Millisecond {
		t.Fatalf("Hold = %s, want >= 400ms for open-tail continuation", hint.Hold)
	}
	if hint.ShouldCommit {
		t.Fatalf("ShouldCommit = true, want false")
	}
}

func TestBuildSemanticEndpointHintContinuationAuxTail(t *testing.T) {
	hint, ok := buildSemanticEndpointHint("we can", 0.8, 900*time.Millisecond)
	if !ok {
		t.Fatalf("buildSemanticEndpointHint() ok=false, want true")
	}
	if hint.Reason != "continuation" {
		t.Fatalf("Reason = %q, want %q", hint.Reason, "continuation")
	}
	if hint.Hold < 500*time.Millisecond {
		t.Fatalf("Hold = %s, want >= 500ms for aux-tail continuation", hint.Hold)
	}
	if hint.ShouldCommit {
		t.Fatalf("ShouldCommit = true, want false")
	}
}

func TestBuildSemanticEndpointHintContinuationIntentTail(t *testing.T) {
	hint, ok := buildSemanticEndpointHint("i want to", 0.79, 1200*time.Millisecond)
	if !ok {
		t.Fatalf("buildSemanticEndpointHint() ok=false, want true")
	}
	if hint.Reason != "continuation" {
		t.Fatalf("Reason = %q, want %q", hint.Reason, "continuation")
	}
	if hint.Hold < 500*time.Millisecond {
		t.Fatalf("Hold = %s, want >= 500ms for intent-tail continuation", hint.Hold)
	}
	if hint.ShouldCommit {
		t.Fatalf("ShouldCommit = true, want false")
	}
}

func TestBuildSemanticEndpointHintTerminal(t *testing.T) {
	hint, ok := buildSemanticEndpointHint("that is all.", 0.84, 2*time.Second)
	if !ok {
		t.Fatalf("buildSemanticEndpointHint() ok=false, want true")
	}
	if hint.Reason != "terminal" {
		t.Fatalf("Reason = %q, want %q", hint.Reason, "terminal")
	}
	if hint.Hold > 150*time.Millisecond {
		t.Fatalf("Hold = %s, want <= 150ms for terminal", hint.Hold)
	}
	if !hint.ShouldCommit {
		t.Fatalf("ShouldCommit = false, want true")
	}
}

func TestBuildSemanticEndpointHintLowConfidenceSuppressesCommit(t *testing.T) {
	hint, ok := buildSemanticEndpointHint("done.", 0.22, 2*time.Second)
	if !ok {
		t.Fatalf("buildSemanticEndpointHint() ok=false, want true")
	}
	if hint.ShouldCommit {
		t.Fatalf("ShouldCommit = true, want false for low confidence")
	}
	if hint.Reason != "low_confidence" {
		t.Fatalf("Reason = %q, want %q", hint.Reason, "low_confidence")
	}
}

func TestSemanticEndpointDispatchState(t *testing.T) {
	var s semanticEndpointDispatchState
	now := time.Now()
	base := semanticEndpointHint{
		Reason:       "continuation",
		Confidence:   0.81,
		Hold:         500 * time.Millisecond,
		ShouldCommit: false,
	}
	if !s.ShouldEmit(base, now) {
		t.Fatalf("ShouldEmit(first) = false, want true")
	}
	if s.ShouldEmit(base, now.Add(200*time.Millisecond)) {
		t.Fatalf("ShouldEmit(unchanged quick) = true, want false")
	}
	next := base
	next.Reason = "terminal"
	next.Hold = 90 * time.Millisecond
	next.ShouldCommit = true
	if !s.ShouldEmit(next, now.Add(300*time.Millisecond)) {
		t.Fatalf("ShouldEmit(changed) = false, want true")
	}
}
