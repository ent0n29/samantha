package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, strings.TrimSpace(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := initTaskSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{pool: pool}, nil
}

func initTaskSchema(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			intent_text TEXT NOT NULL,
			summary TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			risk_level TEXT NOT NULL,
			requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
			current_step_id TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			started_at TIMESTAMPTZ NULL,
			ended_at TIMESTAMPTZ NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_session_created ON tasks (session_id, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS task_steps (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			risk_level TEXT NOT NULL,
			requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
			tool_name TEXT NOT NULL DEFAULT '',
			input_redacted TEXT NOT NULL DEFAULT '',
			output_redacted TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NULL,
			ended_at TIMESTAMPTZ NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_task_steps_task_seq ON task_steps (task_id, seq);`,
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("init task schema failed on %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *PostgresStore) SaveTask(ctx context.Context, task Task) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO tasks (
			id, session_id, user_id, intent_text, summary, mode, priority, status, risk_level,
			requires_approval, current_step_id, result, error, created_at, updated_at, started_at, ended_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
		)
		ON CONFLICT (id) DO UPDATE SET
			session_id=EXCLUDED.session_id,
			user_id=EXCLUDED.user_id,
			intent_text=EXCLUDED.intent_text,
			summary=EXCLUDED.summary,
			mode=EXCLUDED.mode,
			priority=EXCLUDED.priority,
			status=EXCLUDED.status,
			risk_level=EXCLUDED.risk_level,
			requires_approval=EXCLUDED.requires_approval,
			current_step_id=EXCLUDED.current_step_id,
			result=EXCLUDED.result,
			error=EXCLUDED.error,
			created_at=EXCLUDED.created_at,
			updated_at=EXCLUDED.updated_at,
			started_at=EXCLUDED.started_at,
			ended_at=EXCLUDED.ended_at`,
		task.ID,
		task.SessionID,
		task.UserID,
		task.IntentText,
		task.Summary,
		task.Mode,
		task.Priority,
		string(task.Status),
		string(task.RiskLevel),
		task.RequiresApproval,
		task.CurrentStepID,
		task.Result,
		task.Error,
		task.CreatedAt,
		task.UpdatedAt,
		task.StartedAt,
		task.EndedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert task: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM task_steps WHERE task_id=$1`, task.ID); err != nil {
		return fmt.Errorf("delete prior steps: %w", err)
	}

	for _, step := range task.Steps {
		_, err := tx.Exec(ctx,
			`INSERT INTO task_steps (
				id, task_id, seq, title, status, risk_level, requires_approval, tool_name,
				input_redacted, output_redacted, error, started_at, ended_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13
			)`,
			step.ID,
			task.ID,
			step.Seq,
			step.Title,
			string(step.Status),
			string(step.RiskLevel),
			step.RequiresApproval,
			step.ToolName,
			step.InputRedacted,
			step.OutputRedacted,
			step.Error,
			step.StartedAt,
			step.EndedAt,
		)
		if err != nil {
			return fmt.Errorf("insert task step: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetTask(ctx context.Context, taskID string) (Task, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, session_id, user_id, intent_text, summary, mode, priority, status, risk_level,
		        requires_approval, current_step_id, result, error, created_at, updated_at, started_at, ended_at
		   FROM tasks WHERE id=$1`,
		taskID,
	)
	task, err := scanTaskRow(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Task{}, ErrStoreNotFound
		}
		return Task{}, fmt.Errorf("get task: %w", err)
	}
	task.Steps, err = s.loadSteps(ctx, task.ID)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *PostgresStore) ListTasksBySession(ctx context.Context, sessionID string, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, session_id, user_id, intent_text, summary, mode, priority, status, risk_level,
		        requires_approval, current_step_id, result, error, created_at, updated_at, started_at, ended_at
		   FROM tasks WHERE session_id=$1 ORDER BY created_at DESC LIMIT $2`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	out := make([]Task, 0, limit)
	for rows.Next() {
		task, err := scanTaskRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		steps, err := s.loadSteps(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		task.Steps = steps
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task rows: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) loadSteps(ctx context.Context, taskID string) ([]TaskStep, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, seq, title, status, risk_level, requires_approval, tool_name, input_redacted,
		        output_redacted, error, started_at, ended_at
		   FROM task_steps WHERE task_id=$1 ORDER BY seq ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list task steps: %w", err)
	}
	defer rows.Close()

	steps := make([]TaskStep, 0, 4)
	for rows.Next() {
		var (
			step            TaskStep
			status          string
			risk            string
			startedNullable *time.Time
			endedNullable   *time.Time
		)
		if err := rows.Scan(
			&step.ID,
			&step.Seq,
			&step.Title,
			&status,
			&risk,
			&step.RequiresApproval,
			&step.ToolName,
			&step.InputRedacted,
			&step.OutputRedacted,
			&step.Error,
			&startedNullable,
			&endedNullable,
		); err != nil {
			return nil, fmt.Errorf("scan task step: %w", err)
		}
		step.TaskID = taskID
		step.Status = StepStatus(status)
		step.RiskLevel = RiskLevel(risk)
		step.StartedAt = startedNullable
		step.EndedAt = endedNullable
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task step rows: %w", err)
	}
	return steps, nil
}

func scanTaskRow(row pgx.Row) (Task, error) {
	var (
		task            Task
		status          string
		risk            string
		startedNullable *time.Time
		endedNullable   *time.Time
	)
	if err := row.Scan(
		&task.ID,
		&task.SessionID,
		&task.UserID,
		&task.IntentText,
		&task.Summary,
		&task.Mode,
		&task.Priority,
		&status,
		&risk,
		&task.RequiresApproval,
		&task.CurrentStepID,
		&task.Result,
		&task.Error,
		&task.CreatedAt,
		&task.UpdatedAt,
		&startedNullable,
		&endedNullable,
	); err != nil {
		return Task{}, err
	}
	task.Status = TaskStatus(status)
	task.RiskLevel = RiskLevel(risk)
	task.StartedAt = startedNullable
	task.EndedAt = endedNullable
	return task, nil
}

func scanTaskRows(rows pgx.Rows) (Task, error) {
	var (
		task            Task
		status          string
		risk            string
		startedNullable *time.Time
		endedNullable   *time.Time
	)
	if err := rows.Scan(
		&task.ID,
		&task.SessionID,
		&task.UserID,
		&task.IntentText,
		&task.Summary,
		&task.Mode,
		&task.Priority,
		&status,
		&risk,
		&task.RequiresApproval,
		&task.CurrentStepID,
		&task.Result,
		&task.Error,
		&task.CreatedAt,
		&task.UpdatedAt,
		&startedNullable,
		&endedNullable,
	); err != nil {
		return Task{}, err
	}
	task.Status = TaskStatus(status)
	task.RiskLevel = RiskLevel(risk)
	task.StartedAt = startedNullable
	task.EndedAt = endedNullable
	return task, nil
}

func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}
