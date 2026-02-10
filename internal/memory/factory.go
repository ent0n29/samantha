package memory

import (
	"context"
	"strings"
)

// NewStore creates a postgres-backed store when configured, otherwise in-memory.
func NewStore(ctx context.Context, databaseURL string) (Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return NewInMemoryStore(), nil
	}
	return NewPostgresStore(ctx, databaseURL)
}
