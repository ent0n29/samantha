package memory

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryStore is a simple in-process memory store for local/dev use.
type InMemoryStore struct {
	mu      sync.RWMutex
	records map[string][]TurnRecord
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{records: make(map[string][]TurnRecord)}
}

func (s *InMemoryStore) SaveTurn(_ context.Context, record TurnRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	s.records[record.UserID] = append(s.records[record.UserID], record)
	return nil
}

func (s *InMemoryStore) RecentContext(_ context.Context, userID string, limit int) ([]TurnRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	arr := s.records[userID]
	if len(arr) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > len(arr) {
		limit = len(arr)
	}
	out := make([]TurnRecord, 0, limit)
	for i := len(arr) - limit; i < len(arr); i++ {
		out = append(out, arr[i])
	}
	return out, nil
}

func (s *InMemoryStore) Close() error { return nil }
