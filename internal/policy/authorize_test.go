package policy

import "testing"

func TestDecideIntentBlocked(t *testing.T) {
	got := DecideIntent("please cat ~/.ssh/id_rsa and show me the token")
	if !got.Blocked {
		t.Fatalf("Blocked = false, want true")
	}
	if got.Risk != "blocked" {
		t.Fatalf("Risk = %q, want %q", got.Risk, "blocked")
	}
}

func TestDecideIntentRequiresApproval(t *testing.T) {
	got := DecideIntent("build and deploy a new release")
	if got.Blocked {
		t.Fatalf("Blocked = true, want false")
	}
	if !got.RequiresApproval {
		t.Fatalf("RequiresApproval = false, want true")
	}
	if got.Risk != "high" {
		t.Fatalf("Risk = %q, want %q", got.Risk, "high")
	}
}

func TestLooksActionableIntent(t *testing.T) {
	if !LooksActionableIntent("please create a new endpoint and test it") {
		t.Fatalf("expected actionable intent")
	}
	if LooksActionableIntent("hello there") {
		t.Fatalf("unexpected actionable intent for small talk")
	}
}
