package voice

import "testing"

func TestLeadResponseFilterStripsSingleChunkFillerPrefix(t *testing.T) {
	f := newLeadResponseFilter()
	got := f.Consume("Give me a second while I think. We can ship this today.")
	want := "We can ship this today."
	if got != want {
		t.Fatalf("Consume() = %q, want %q", got, want)
	}
}

func TestLeadResponseFilterStripsSplitFillerPrefix(t *testing.T) {
	f := newLeadResponseFilter()
	if got := f.Consume("Give me a sec"); got != "" {
		t.Fatalf("Consume(part1) = %q, want empty", got)
	}
	if got := f.Consume("ond while I think."); got != "" {
		t.Fatalf("Consume(part2) = %q, want empty", got)
	}
	got := f.Consume(" Let's do it.")
	want := " Let's do it."
	if got != want {
		t.Fatalf("Consume(part3) = %q, want %q", got, want)
	}
}

func TestLeadResponseFilterKeepsNonFillerText(t *testing.T) {
	f := newLeadResponseFilter()
	got := f.Consume("Let's begin with the architecture.")
	want := "Let's begin with the architecture."
	if got != want {
		t.Fatalf("Consume() = %q, want %q", got, want)
	}
}

func TestLeadResponseFilterFinalizeUsesFallbackWhenStreamSilent(t *testing.T) {
	f := newLeadResponseFilter()
	if got := f.Consume("Give me a second."); got != "" {
		t.Fatalf("Consume() = %q, want empty", got)
	}
	got := f.Finalize("Give me a second. Here is the answer.")
	want := "Here is the answer."
	if got != want {
		t.Fatalf("Finalize() = %q, want %q", got, want)
	}
}

func TestStripAssistantLeadFillerDoesNotStripSecondChance(t *testing.T) {
	got := stripAssistantLeadFiller("Give me a second chance to explain.")
	want := "Give me a second chance to explain."
	if got != want {
		t.Fatalf("stripAssistantLeadFiller() = %q, want %q", got, want)
	}
}
