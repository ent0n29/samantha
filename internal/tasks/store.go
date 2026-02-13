package tasks

import (
	"context"
	"errors"
)

var ErrStoreNotFound = errors.New("task not found in store")

type Store interface {
	SaveTask(ctx context.Context, task Task) error
	GetTask(ctx context.Context, taskID string) (Task, error)
	ListTasksBySession(ctx context.Context, sessionID string, limit int) ([]Task, error)
	Close() error
}
