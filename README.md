# Samantha

Talk to your computer like you talk to a friend, and let your agents build.

Voice-first companion UI for OpenClaw. Local-first and Apple Silicon friendly.

<div align="center">
  <img src="./docs/media/sam.gif" alt="Samantha demo preview" width="640" />
  <p><strong>Build by voice. Ship at thought speed.</strong></p>
</div>

Samantha is a small Go HTTP/WebSocket server plus a browser UI that does:

`microphone audio` -> `STT` -> `OpenClaw (brain)` -> `streaming TTS` -> `speaker`

If OpenClaw isn't configured, it falls back to a deterministic mock "brain" so you can iterate on the voice loop.

See `docs/ARCHITECTURE.md` for the component map and data flow.
API reference: `docs/API.md`.

## Architecture

```mermaid
%%{init: {
  "theme": "base",
  "themeVariables": {
    "background": "transparent",
    "lineColor": "#9fc8ff",
    "primaryTextColor": "#f3f9ff",
    "edgeLabelBackground": "#0b1524",
    "fontSize": "15px",
    "fontFamily": "ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif"
  }
}}%%
flowchart LR
  subgraph Client["Browser UI (/ui/)"]
    Mic["Mic capture"]
    UX["Captions + audio playback"]
  end

  subgraph Server["Samantha server (:8080)"]
    API["HTTP + WebSocket API"]
    Orch["Voice orchestrator"]
    Sess["Session lifecycle"]
    Task["Task runtime"]
  end

  subgraph Voice["Voice providers"]
    STT["STT (local / ElevenLabs / mock)"]
    TTS["TTS (local / ElevenLabs / mock)"]
  end

  subgraph Brain["OpenClaw brain"]
    Adapter["Adapter (auto / cli / http / mock)"]
    Agent["Agent workspace"]
  end

  subgraph Storage["Memory + persistence"]
    Mem["In-memory store"]
    PG[("Postgres (optional)")]
  end

  Mic -->|"16kHz PCM chunks"| API
  API --> Orch
  Orch --> STT
  STT -->|"partial + committed transcripts"| Orch
  Orch --> Adapter
  Adapter --> Agent
  Adapter -->|"response deltas"| Orch
  Orch --> TTS
  TTS -->|"audio stream"| API
  API --> UX
  Orch --> Sess
  Orch --> Mem
  Orch --> Task
  Mem -. "when DATABASE_URL" .-> PG
  Task -. "task snapshots" .-> PG

  classDef node fill:#0e1a2c,stroke:#9fc8ff,color:#f3f9ff,stroke-width:2px;
  classDef store fill:#102537,stroke:#7ee7ff,color:#effdff,stroke-width:2px;
  class Mic,UX,API,Orch,Sess,Task,STT,TTS,Adapter,Agent node;
  class Mem,PG store;

  style Client fill:#0a1423,stroke:#416a94,color:#9fd7ff,stroke-width:2px;
  style Server fill:#0a1423,stroke:#416a94,color:#9fd7ff,stroke-width:2px;
  style Voice fill:#0a1423,stroke:#416a94,color:#9fd7ff,stroke-width:2px;
  style Brain fill:#0a1423,stroke:#416a94,color:#9fd7ff,stroke-width:2px;
  style Storage fill:#0a1423,stroke:#416a94,color:#9fd7ff,stroke-width:2px;

```

## Quickstart (macOS)

```bash
make dev
```

Open:

- `http://127.0.0.1:8080/ui/` (add `?onboarding=1` to rerun first-run checks)

## Configuration

Copy `.env.example` to `.env` and tweak as needed. Key vars:

- `VOICE_PROVIDER=local|auto|elevenlabs|mock` (default `local`)
  - `local`: OSS/offline-first default.
  - `elevenlabs`: use ElevenLabs directly (requires credits/API key).
  - `auto`: prefers ElevenLabs when configured, with automatic runtime fallback to local voice if ElevenLabs session/stream startup fails.
- `APP_SESSION_RETENTION` (ended-session retention window before pruning)
- `APP_STRICT_OUTBOUND` + `APP_WS_BACKPRESSURE_MODE=drop|block`
- `APP_UI_AUDIO_WORKLET` (attempt low-latency AudioWorklet mic capture in `/ui/`, with fallback)
- `APP_ASSISTANT_WORKING_DELAY` (backend delay before emitting `assistant_working`; `0` disables)
- `APP_UI_SILENCE_BREAKER_MODE=off|visual|speech` (dead-air behavior while waiting)
- `APP_UI_SILENCE_BREAKER_DELAY` (delay before silence-breaker triggers after `assistant_working`)
- `APP_UI_TASK_DESK_DEFAULT` (keep Task Desk hidden by default in core `/ui/`)
- `APP_TASK_RUNTIME_ENABLED`, `APP_TASK_TIMEOUT`, `APP_TASK_IDEMPOTENCY_WINDOW` (voice-to-task runtime)
- `OPENCLAW_ADAPTER_MODE=auto|cli|http|mock`
- `OPENCLAW_HTTP_URL` (when using `http`)
- `OPENCLAW_HTTP_STREAM_STRICT` (strict streamed JSON validation for OpenClaw HTTP adapter)
- `OPENCLAW_AGENT_ID` (OpenClaw agent id; default `samantha` in `make dev`)
- `OPENCLAW_WORKSPACE_DIR` (override workspace path; for new agents defaults to `~/.openclaw/workspaces/$OPENCLAW_AGENT_ID`)
- `OPENCLAW_CLI_PATH` (when using `cli` or `auto`)
- `OPENCLAW_CLI_THINKING=minimal|low|medium|high` (lower is faster first response; default `low`)
- `OPENCLAW_CLI_STREAMING=true|false` (feature-flag incremental CLI text streaming; default `true`)
- `OPENCLAW_CLI_STREAM_MIN_CHARS` (chunking threshold for incremental CLI streaming; default `16`)
- `DATABASE_URL` (optional; enables Postgres-backed memory)
  - Also persists task runtime state (`tasks`, `task_steps`) when task runtime is enabled.

## Voice Backends

- Local (offline, default): whisper.cpp STT + Kokoro TTS (`VOICE_PROVIDER=local`)
  - First run: `make setup-local-voice`
- ElevenLabs (optional): set `VOICE_PROVIDER=elevenlabs` + `ELEVENLABS_API_KEY`
  - If you want automatic fallback to local when ElevenLabs fails, use `VOICE_PROVIDER=auto`.
  - For lowest-latency playback, keep `ELEVENLABS_TTS_OUTPUT_FORMAT=pcm_16000` (default).
  - If you want the backend to auto-decide when to commit transcripts, set `ELEVENLABS_STT_COMMIT_STRATEGY=vad` (default is `manual`, driven by the UI).

## Brain (OpenClaw)

By default, `make dev` will:

- Attempt to bootstrap OpenClaw auth from your Codex login (if present).
- Ensure a local OpenClaw agent exists (new agents default to `~/.openclaw/workspaces/$OPENCLAW_AGENT_ID`; existing agents keep their configured workspace unless you set `OPENCLAW_WORKSPACE_DIR`).
- Sync the repo’s workspace template in `openclaw/samantha-workspace/` into the active agent workspace each run.

If `openclaw` is not installed/configured, the server runs with a mock brain.

## Performance Loop

- Live latency snapshot API: `GET /v1/perf/latency`
- Probe script:
  - `make perf-latency`
  - or `INTERVAL_SEC=1 SAMPLES=30 ./scripts/perf_latency_probe.sh http://127.0.0.1:8080`
