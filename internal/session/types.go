package session

import "time"

// CreateRequest defines payload for creating a new session.
type CreateRequest struct {
	UserID    string `json:"user_id"`
	PersonaID string `json:"persona_id"`
	VoiceID   string `json:"voice_id"`
}

// CreateResponse returns created session metadata.
type CreateResponse struct {
	SessionID       string    `json:"session_id"`
	UserID          string    `json:"user_id"`
	Status          Status    `json:"status"`
	PersonaID       string    `json:"persona_id"`
	VoiceID         string    `json:"voice_id"`
	StartedAt       time.Time `json:"started_at"`
	LastActivityAt  time.Time `json:"last_activity_at"`
	InactivityTTLMS int64     `json:"inactivity_ttl_ms"`
}
