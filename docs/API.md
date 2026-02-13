# API

Samantha exposes a small REST API plus a single WebSocket for realtime voice.

Base URL defaults to `http://127.0.0.1:8080`.

## REST

### `POST /v1/voice/session`

Creates a new voice session.

Request body (optional):
```json
{
  "user_id": "anonymous",
  "persona_id": "warm",
  "voice_id": "af_heart"
}
```

Response (`201`):
```json
{
  "session_id": "...",
  "user_id": "...",
  "status": "active",
  "persona_id": "warm",
  "voice_id": "...",
  "started_at": "...",
  "last_activity_at": "...",
  "inactivity_ttl_ms": 120000
}
```

### `POST /v1/voice/session/{id}/end`

Ends a session.

### `GET /v1/voice/session/ws?session_id=...`

Upgrades to a WebSocket for streaming audio and events (see below).

### `GET /v1/onboarding/status`

Returns setup checks for voice + brain providers.

Response (`200`):
```json
{
  "voice_provider": "local",
  "brain_provider": "cli",
  "task_runtime_enabled": true,
  "task_store_mode": "postgres",
  "ui_audio_worklet": true,
  "checks": [
    {
      "id": "voice_provider",
      "status": "ok",
      "label": "Voice backend",
      "detail": "local"
    }
  ]
}
```

### `GET /v1/perf/latency`

Returns a rolling in-memory latency snapshot (recent window) for key turn stages.

Response (`200`):
```json
{
  "generated_at": "2026-02-12T18:24:10.410157Z",
  "window_size": 256,
  "stages": [
    {
      "stage": "commit_to_first_audio",
      "samples": 48,
      "last_ms": 612,
      "avg_ms": 544.2,
      "p50_ms": 521,
      "p95_ms": 812,
      "p99_ms": 1029,
      "target_p95_ms": 900
    }
  ]
}
```

### `GET /v1/voice/voices`

Lists available voices for the configured backend.

### `POST /v1/voice/tts/preview`

Generates a short audio preview for the selected voice.

Request body:
```json
{
  "voice_id": "af_heart",
  "persona_id": "warm",
  "text": "Hi. I'm here with you."
}
```

Response: raw audio bytes (`audio/wav`, `audio/mpeg`, or `application/octet-stream`).

### `POST /v1/tasks`

Creates a durable task for the given session.

Request body:
```json
{
  "session_id": "...",
  "user_id": "anonymous",
  "intent_text": "build a landing page and deploy it",
  "mode": "auto",
  "priority": "normal"
}
```

Response (`201`):
```json
{
  "task_id": "...",
  "status": "planned",
  "requires_approval": false,
  "summary": "build a landing page and deploy it",
  "deduped": false
}
```

### `POST /v1/tasks/{id}/approve`

Approves or denies a task waiting for approval.

Request body:
```json
{
  "approved": true
}
```

### `GET /v1/tasks/{id}`

Returns task details and step-level status.

### `GET /v1/tasks?session_id=...&limit=20`

Lists recent tasks for a session.

### `POST /v1/tasks/{id}/cancel`

Cancels a task.

Request body (optional):
```json
{
  "reason": "Cancelled by API."
}
```

### `GET /v1/tasks/{id}/events?limit=100`

Returns recent event history for one task (oldest to newest, capped by `limit`).

## WebSocket Protocol

Client sends:

- `client_audio_chunk`: base64 PCM16 mono at 16kHz (recommended).
```json
{
  "type": "client_audio_chunk",
  "session_id": "...",
  "seq": 1,
  "pcm16_base64": "...",
  "sample_rate": 16000,
  "ts_ms": 0
}
```

- `client_control`: session controls.
```json
{
  "type": "client_control",
  "session_id": "...",
  "action": "stop"
}
```

Known `action` values:
- `stop`: request an STT commit
- `interrupt`: cancel the current assistant turn (barge-in)
- `wakeword_on`, `wakeword_off`
- `manual_arm`: allow one wake-word-bypassed utterance in hands-free mode
- `approve_task_step`, `deny_task_step`, `cancel_task`

Server sends:

- `stt_partial`
- `stt_committed`
- `assistant_text_delta` (streaming assistant text)
- `assistant_audio_chunk` (base64 audio)
- `assistant_turn_end`
- `system_event` (e.g. `wake_word`)
- `error_event`
- `task_created`
- `task_plan_delta`
- `task_step_started`
- `task_step_log`
- `task_step_completed`
- `task_waiting_approval`
- `task_completed`
- `task_failed`
- `task_status_snapshot` (sent once on websocket connect when task runtime is enabled)

Message schemas live in `internal/protocol/messages.go`.
