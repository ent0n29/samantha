package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerCreateGetEnd(t *testing.T) {
	m := NewManager(time.Minute)
	s := m.Create("u1", "warm", "")
	if s.ID == "" {
		t.Fatalf("session ID should not be empty")
	}

	got, err := m.Get(s.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.UserID != "u1" || got.PersonaID != "warm" || got.Status != StatusActive {
		t.Fatalf("unexpected session state: %+v", got)
	}

	ended, err := m.End(s.ID)
	if err != nil {
		t.Fatalf("End() error = %v", err)
	}
	if ended.Status != StatusEnded {
		t.Fatalf("ended status = %q, want %q", ended.Status, StatusEnded)
	}
}

func TestManagerInterruptClearsTurn(t *testing.T) {
	m := NewManager(time.Minute)
	s := m.Create("u1", "warm", "")
	if err := m.StartTurn(s.ID, "turn-1"); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := m.Interrupt(s.ID); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}

	got, err := m.Get(s.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ActiveTurnID != "" {
		t.Fatalf("ActiveTurnID = %q, want empty", got.ActiveTurnID)
	}
	if got.InterruptionCount != 1 {
		t.Fatalf("InterruptionCount = %d, want 1", got.InterruptionCount)
	}
}

func TestManagerJanitorExpiresInactive(t *testing.T) {
	m := NewManager(30 * time.Millisecond)
	s := m.Create("u1", "warm", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.StartJanitor(ctx, 10*time.Millisecond)

	time.Sleep(90 * time.Millisecond)
	got, err := m.Get(s.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != StatusEnded {
		t.Fatalf("Status = %q, want %q", got.Status, StatusEnded)
	}
}

func TestManagerPrunesEndedSessionsAfterRetention(t *testing.T) {
	m := NewManager(time.Minute)
	m.SetEndedRetention(50 * time.Millisecond)
	s := m.Create("u1", "warm", "")
	if _, err := m.End(s.ID); err != nil {
		t.Fatalf("End() error = %v", err)
	}

	m.mu.Lock()
	m.sessions[s.ID].LastActivityAt = time.Now().Add(-time.Second)
	m.mu.Unlock()
	m.expireInactive()

	if _, err := m.Get(s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotFound)
	}
}

func TestManagerRetentionZeroDisablesEndedPruning(t *testing.T) {
	m := NewManager(time.Minute)
	m.SetEndedRetention(0)
	s := m.Create("u1", "warm", "")
	if _, err := m.End(s.ID); err != nil {
		t.Fatalf("End() error = %v", err)
	}

	m.mu.Lock()
	m.sessions[s.ID].LastActivityAt = time.Now().Add(-24 * time.Hour)
	m.mu.Unlock()
	m.expireInactive()

	got, err := m.Get(s.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != StatusEnded {
		t.Fatalf("Status = %q, want %q", got.Status, StatusEnded)
	}
}
