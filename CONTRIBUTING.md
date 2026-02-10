# Contributing

Thanks for helping build Samantha.

This repo is optimized for fast iteration on a voice-first loop:
microphone -> STT -> OpenClaw -> streaming TTS -> speaker.

## Setup

- macOS (recommended for local voice):
  - `make dev`
  - Open `http://127.0.0.1:8080/ui/` (add `?onboarding=1` to rerun first-run checks)

Configuration lives in `.env` (ignored). Start from `.env.example`.

## Common Tasks

- Run the server: `make run`
- Dev (bootstraps OpenClaw if present, sets up local voice if needed): `make dev`
- Local voice bootstrap (whisper.cpp + Kokoro): `make setup-local-voice`
- Tests: `make test`
- Format: `make fmt`

## Project Map

- `cmd/samantha/`: entrypoint
- `internal/app/`: composition root (wires providers + server)
- `internal/httpapi/`: REST + WebSocket API and embedded UI
- `internal/voice/`: orchestrator + STT/TTS interfaces and providers
- `internal/openclaw/`: OpenClaw adapters (CLI/HTTP/mock)
- `internal/memory/`: conversation memory store (in-memory by default; Postgres optional)
- `openclaw/samantha-workspace/`: versioned workspace template (personality). Runtime workspace lives outside the repo (see `.env.example`).

Architecture notes: `docs/ARCHITECTURE.md`

## Pull Requests

- Keep PRs small and scoped.
- Add tests for new parsing logic and adapter behavior.
- Avoid committing local artifacts: `.env`, `.venv`, `.tools`, `.models`, `.idea`, large binaries.

## Security

- Never commit secrets (API keys, tokens, auth profiles).
- Do not add any client-side code that embeds long-lived keys.
- Prefer server-minted single-use tokens for browser realtime integrations.
