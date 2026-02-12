package observability

import "testing"

func TestTurnStageWindowSnapshot(t *testing.T) {
	w := newTurnStageWindow(8)
	w.Observe("commit_to_first_audio", 500)
	w.Observe("commit_to_first_audio", 700)
	w.Observe("commit_to_first_audio", 900)

	snap := w.Snapshot()
	if snap.WindowSize != 8 {
		t.Fatalf("WindowSize = %d, want 8", snap.WindowSize)
	}
	if len(snap.Stages) != 1 {
		t.Fatalf("len(Stages) = %d, want 1", len(snap.Stages))
	}
	s := snap.Stages[0]
	if s.Stage != "commit_to_first_audio" {
		t.Fatalf("Stage = %q, want %q", s.Stage, "commit_to_first_audio")
	}
	if s.Samples != 3 {
		t.Fatalf("Samples = %d, want 3", s.Samples)
	}
	if s.LastMS != 900 {
		t.Fatalf("LastMS = %.2f, want 900", s.LastMS)
	}
	if s.P50MS != 700 {
		t.Fatalf("P50MS = %.2f, want 700", s.P50MS)
	}
	if s.P95MS <= 700 || s.P95MS > 900 {
		t.Fatalf("P95MS = %.2f, want (700,900]", s.P95MS)
	}
	if s.TargetP95MS != 900 {
		t.Fatalf("TargetP95MS = %.2f, want 900", s.TargetP95MS)
	}
}
