# Samantha

Voice-first companion UI for OpenClaw (inspired by *Her*). Local-first and Apple Silicon friendly.

Samantha is a small Go HTTP/WebSocket server plus a browser UI that does:

`microphone audio` -> `STT` -> `OpenClaw (brain)` -> `streaming TTS` -> `speaker`

If OpenClaw isn't configured, it falls back to a deterministic mock "brain" so you can iterate on the voice loop.

See `docs/ARCHITECTURE.md` for the component map and data flow.
API reference: `docs/API.md`.

## Quickstart (macOS)

```bash
make dev
```

Open:

- `http://127.0.0.1:8080/ui/` (add `?onboarding=1` to rerun first-run checks)

## Configuration

Copy `.env.example` to `.env` and tweak as needed. Key vars:

- `VOICE_PROVIDER=auto|local|elevenlabs|mock`
- `OPENCLAW_ADAPTER_MODE=auto|cli|http|mock`
- `OPENCLAW_HTTP_URL` (when using `http`)
- `OPENCLAW_AGENT_ID` (OpenClaw agent id; default `samantha` in `make dev`)
- `OPENCLAW_WORKSPACE_DIR` (per-user OpenClaw workspace; defaults to `~/.openclaw/workspaces/$OPENCLAW_AGENT_ID`)
- `OPENCLAW_CLI_PATH` (when using `cli` or `auto`)
- `DATABASE_URL` (optional; enables Postgres-backed memory)

## Voice Backends

- Local (offline): whisper.cpp STT + Kokoro TTS (`VOICE_PROVIDER=local`)
  - First run: `make setup-local-voice`
- ElevenLabs (optional): set `VOICE_PROVIDER=elevenlabs` + `ELEVENLABS_API_KEY`
  - For lowest-latency playback, keep `ELEVENLABS_TTS_OUTPUT_FORMAT=pcm_16000` (default).
  - If you want the backend to auto-decide when to commit transcripts, set `ELEVENLABS_STT_COMMIT_STRATEGY=vad` (default is `manual`, driven by the UI).

## Brain (OpenClaw)

By default, `make dev` will:

- Attempt to bootstrap OpenClaw auth from your Codex login (if present).
- Ensure a local OpenClaw agent exists using a per-user workspace (default: `~/.openclaw/workspaces/$OPENCLAW_AGENT_ID`).
- Sync the repo’s workspace template in `openclaw/samantha-workspace/` into that per-user workspace each run.

If `openclaw` is not installed/configured, the server runs with a mock brain.
