# Repository Guidelines

## Project Structure
- `cmd/samantha/`: Go entrypoint for the voice companion HTTP/WebSocket server.
- `internal/voice/`: Realtime orchestration (STT events -> LLM -> TTS streaming).
- `internal/voice/elevenlabs.go`: ElevenLabs realtime STT/TTS provider.
- `internal/openclaw/`: “Brain” adapters (CLI/HTTP/mock) used by the orchestrator.
- `internal/httpapi/`: REST + WebSocket API and embedded UI (`internal/httpapi/static/` served at `/ui/`).
- `internal/memory/`: In-memory + optional Postgres-backed conversation memory store.
- `migrations/`: Postgres migrations (only used when `DATABASE_URL` is set).
- `openclaw/samantha-workspace/`: OpenClaw agent workspace that tunes the assistant’s personality.

## Build, Test, and Development Commands
- `make dev`: Bootstrap OpenClaw from Codex login (if present) and start the server on `:8080`.
- `make run`: Run the server.
- `make build`: Build `bin/samantha`.
- `make test`: Run unit tests.
- `make fmt`: Run `go fmt ./...` (formatting).

Open the UI at `http://127.0.0.1:8080/ui/`.

## Coding Style & Naming
- Go: standard `gofmt` formatting; keep packages small and cohesive under `internal/`.
- Prefer explicit, typed events (`protocol/*`) over ad-hoc `map[string]any`.
- Naming: `NewXxx(...)` constructors; `*_test.go` for unit tests.

## Testing Guidelines
- Framework: Go standard library `testing`.
- Add tests for: message parsing (`internal/protocol`), adapter behavior (`internal/openclaw`), and config defaults (`internal/config`).
- Run: `make test`.

## Commit & Pull Request Guidelines
- This workspace may not include git history. Default to Conventional Commits (`feat:`, `fix:`, `chore:`).
- PRs should include: what changed, how to run/test, and any UX screenshots for `/ui/` changes.

## Security & Local Config
- Never commit secrets. Use `.env` (local) or exported env vars; see `.env.example`.
- `scripts/openclaw_bootstrap_from_codex.py` syncs Codex OAuth tokens into OpenClaw local state so OpenClaw can run without an OpenAI API key.
