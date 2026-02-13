package tasks

import (
	"context"
	"strings"
)

func NewStore(ctx context.Context, databaseURL string) (Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, nil
	}
	return NewPostgresStore(ctx, databaseURL)
}
