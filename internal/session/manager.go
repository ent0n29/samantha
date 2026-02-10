package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive Status = "active"
	StatusEnded  Status = "ended"
)

var ErrNotFound = errors.New("session not found")

type Session struct {
	ID                string    `json:"session_id"`
	UserID            string    `json:"user_id"`
	Status            Status    `json:"status"`
	PersonaID         string    `json:"persona_id"`
	VoiceID           string    `json:"voice_id"`
	ActiveTurnID      string    `json:"active_turn_id"`
	InterruptionCount int       `json:"interruption_count"`
	StartedAt         time.Time `json:"started_at"`
	LastActivityAt    time.Time `json:"last_activity_at"`
}

type Manager struct {
	mu                sync.RWMutex
	sessions          map[string]*Session
	sessionByUser     map[string]string
	inactivityTimeout time.Duration
	onExpire          func(*Session)
}

func NewManager(inactivityTimeout time.Duration) *Manager {
	if inactivityTimeout <= 0 {
		inactivityTimeout = 2 * time.Minute
	}
	return &Manager{
		sessions:          make(map[string]*Session),
		sessionByUser:     make(map[string]string),
		inactivityTimeout: inactivityTimeout,
	}
}

func (m *Manager) SetExpireHook(hook func(*Session)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onExpire = hook
}

func (m *Manager) Create(userID, personaID, voiceID string) *Session {
	now := time.Now().UTC()
	s := &Session{
		ID:             uuid.NewString(),
		UserID:         userID,
		PersonaID:      personaID,
		VoiceID:        voiceID,
		Status:         StatusActive,
		StartedAt:      now,
		LastActivityAt: now,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	if userID != "" {
		m.sessionByUser[userID] = s.ID
	}
	return clone(s)
}

func (m *Manager) Get(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(s), nil
}

func (m *Manager) Touch(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	s.LastActivityAt = time.Now().UTC()
	return nil
}

func (m *Manager) StartTurn(sessionID, turnID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	s.ActiveTurnID = turnID
	s.LastActivityAt = time.Now().UTC()
	return nil
}

func (m *Manager) Interrupt(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	s.InterruptionCount++
	s.ActiveTurnID = ""
	s.LastActivityAt = time.Now().UTC()
	return nil
}

func (m *Manager) End(sessionID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	s.Status = StatusEnded
	s.ActiveTurnID = ""
	s.LastActivityAt = time.Now().UTC()
	if s.UserID != "" {
		delete(m.sessionByUser, s.UserID)
	}
	return clone(s), nil
}

func (m *Manager) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.expireInactive()
			}
		}
	}()
}

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.sessions {
		if s.Status == StatusActive {
			count++
		}
	}
	return count
}

func (m *Manager) expireInactive() {
	now := time.Now().UTC()
	var expired []*Session

	m.mu.Lock()
	for _, s := range m.sessions {
		if s.Status != StatusActive {
			continue
		}
		if now.Sub(s.LastActivityAt) < m.inactivityTimeout {
			continue
		}
		s.Status = StatusEnded
		s.ActiveTurnID = ""
		s.LastActivityAt = now
		expired = append(expired, clone(s))
		if s.UserID != "" {
			delete(m.sessionByUser, s.UserID)
		}
	}
	hook := m.onExpire
	m.mu.Unlock()

	if hook != nil {
		for _, s := range expired {
			hook(s)
		}
	}
}

func clone(s *Session) *Session {
	c := *s
	return &c
}
