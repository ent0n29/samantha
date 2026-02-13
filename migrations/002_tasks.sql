CREATE TABLE IF NOT EXISTS tasks (
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
);

CREATE INDEX IF NOT EXISTS idx_tasks_session_created
ON tasks (session_id, created_at DESC);

CREATE TABLE IF NOT EXISTS task_steps (
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
);

CREATE INDEX IF NOT EXISTS idx_task_steps_task_seq
ON task_steps (task_id, seq);
