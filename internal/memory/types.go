package memory

import (
	"context"
	"time"
)

// TurnRecord stores a single user or assistant conversational turn.
type TurnRecord struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	SessionID   string    `json:"session_id"`
	Role        string    `json:"role"`
	Content     string    `json:"content"`
	PIIRedacted bool      `json:"pii_redacted"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store persists and retrieves conversational memory.
type Store interface {
	SaveTurn(ctx context.Context, record TurnRecord) error
	RecentContext(ctx context.Context, userID string, limit int) ([]TurnRecord, error)
	Close() error
}
