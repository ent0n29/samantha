package voice

import "testing"

func TestCommaBoundaryHonorsMinimumChunkSize(t *testing.T) {
	input := "Let's map the architecture first, then connect voice and tasks."
	if idx := commaBoundary(input, prosodyCommaChunkMin); idx >= 0 {
		t.Fatalf("commaBoundary() = %d, want -1 when comma is before minimum chunk size", idx)
	}
}

func TestNextProsodySegmentSkipsEarlyCommaBoundary(t *testing.T) {
	input := "Let's map the architecture first, then connect the voice loop to task execution and persistence for smoother flow."
	segment, _, ok := nextProsodySegment(input, prosodyFirstChunkMin, false)
	if !ok {
		t.Fatalf("nextProsodySegment() ok=false, want true")
	}
	if len(segment) < prosodyCommaChunkMin {
		t.Fatalf("segment length = %d, want >= %d to avoid choppy comma splits", len(segment), prosodyCommaChunkMin)
	}
	if segment == "Let's map the architecture first," {
		t.Fatalf("segment = %q, want a longer phrase than the first comma clause", segment)
	}
}
