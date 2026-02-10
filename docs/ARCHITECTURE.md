# Architecture

Samantha is a small, voice-first runtime that gives OpenClaw a realtime microphone + speaker loop.

## High-Level Flow

1. Browser UI (`/ui/`) captures mic audio, downsamples to 16kHz PCM16, and streams chunks over WebSocket.
2. Server runs STT, emitting `partial` and `committed` transcripts.
3. On a committed user utterance, the orchestrator:
   - saves the turn to memory (with light PII redaction)
   - fetches recent context
   - streams the user request into OpenClaw (brain)
4. As OpenClaw produces text, the server:
   - forwards text deltas to the UI
   - forwards deltas into a streaming TTS engine
5. The UI plays synthesized audio and shows captions.

## Components

- HTTP + WebSocket gateway: `internal/httpapi/`
- Session lifecycle: `internal/session/`
- Realtime orchestration: `internal/voice/`
- Voice backends:
  - Local: whisper.cpp STT + Kokoro TTS (`internal/voice/local.go`)
  - ElevenLabs: realtime STT/TTS websockets (`internal/voice/elevenlabs.go`)
  - Mock (dev fallback): `internal/voice/mock.go`
- Brain adapters (OpenClaw):
  - CLI: `internal/openclaw/cli.go`
  - HTTP: `internal/openclaw/http.go`
  - Mock: `internal/openclaw/mock.go`
- Memory store:
  - In-memory: `internal/memory/inmemory.go`
  - Postgres (optional): `internal/memory/postgres.go`

## Notes / Constraints

- The system is optimized for tight iteration on latency and "feel", not for production hardening.
- OpenClaw streaming depends on the adapter: HTTP streaming can produce incremental deltas; CLI mode currently returns a single final chunk.

