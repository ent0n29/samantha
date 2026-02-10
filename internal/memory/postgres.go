package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore persists conversational memory in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	if err := initSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	return &PostgresStore{pool: pool}, nil
}

func initSchema(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector;`,
		`CREATE TABLE IF NOT EXISTS memory_items (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			pii_redacted BOOLEAN NOT NULL DEFAULT FALSE,
			embedding vector(1536),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_items_user_created ON memory_items (user_id, created_at);`,
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("init schema failed on %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *PostgresStore) SaveTurn(ctx context.Context, record TurnRecord) error {
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO memory_items (id, user_id, session_id, role, content, pii_redacted, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		record.ID,
		record.UserID,
		record.SessionID,
		record.Role,
		record.Content,
		record.PIIRedacted,
		record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save turn: %w", err)
	}
	return nil
}

func (s *PostgresStore) RecentContext(ctx context.Context, userID string, limit int) ([]TurnRecord, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, session_id, role, content, pii_redacted, created_at
		 FROM memory_items WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`,
		userID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent context: %w", err)
	}
	defer rows.Close()

	items := make([]TurnRecord, 0, limit)
	for rows.Next() {
		var r TurnRecord
		if err := rows.Scan(&r.ID, &r.UserID, &r.SessionID, &r.Role, &r.Content, &r.PIIRedacted, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan context row: %w", err)
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context rows: %w", err)
	}

	// Reverse into chronological order for prompt coherence.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}

	return items, nil
}

func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}
