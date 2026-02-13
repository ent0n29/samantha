package voice

import "testing"

func TestCanonicalizeBrainPrefetchInput(t *testing.T) {
	got := canonicalizeBrainPrefetchInput("  Hey, Samantha! let's build this 🚀 now.  ")
	want := "hey samantha let s build this now"
	if got != want {
		t.Fatalf("canonicalizeBrainPrefetchInput() = %q, want %q", got, want)
	}
}

func TestShouldSpeculateBrainCanonical(t *testing.T) {
	if shouldSpeculateBrainCanonical("short request") {
		t.Fatalf("shouldSpeculateBrainCanonical() = true for short input, want false")
	}

	if shouldSpeculateBrainCanonical("ship this now") {
		t.Fatalf("shouldSpeculateBrainCanonical() = true for short 3-word input, want false")
	}

	if !shouldSpeculateBrainCanonical("please ship this now") {
		t.Fatalf("shouldSpeculateBrainCanonical() = false for long enough 3-word+ input, want true")
	}

	if !shouldSpeculateBrainCanonical("please help me design the next autonomous iteration") {
		t.Fatalf("shouldSpeculateBrainCanonical() = false for long multi-word input, want true")
	}
}
