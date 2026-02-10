# ElevenLabs API Playbook

## Purpose
Notes for the optional ElevenLabs voice backend (`VOICE_PROVIDER=elevenlabs`). This is not the main contributor guide; see `AGENTS.md`.

## Source of Truth
Use official ElevenLabs docs only:
- API reference root: `https://elevenlabs.io/docs/api-reference/introduction`
- Markdown pages for deterministic parsing: append `.md` to docs paths (example: `/docs/api-reference/authentication.md`)
- Changelog: `https://elevenlabs.io/docs/changelog`

When behavior is uncertain, check endpoint `.md` specs first (OpenAPI/AsyncAPI blocks), then changelog.

## Platform Snapshot (verified February 8, 2026)
- API reference pages discovered: 256
- REST operations mapped: 249
- Realtime WebSockets: 3
- Concept pages (non-endpoint): 4 (`introduction`, `authentication`, `streaming`, `studio-api-information`)
- Highest-volume families: `voices`, `studio`, `knowledge-base`, `agents`, `dubbing`, `workspace`

## API Family Inventory (coverage index)
Family counts below are derived from the current API reference crawl and are used to prioritize where contributors should spend review effort.

- `voices` (27) for search, settings, IVC/PVC lifecycle, and voice library sharing
- `studio` (23) for long-form project/chapter generation and snapshot streaming
- `knowledge-base` (21) for ConvAI documents, content retrieval, and RAG index operations
- `agents` (20) for create/update/simulate/deploy plus branch and draft workflows
- `dubbing` (19) for multilingual dubbing and transcript/resource management
- `workspace` (18) for groups, invites, resources, dashboard/settings, and secrets
- `mcp` (13) for MCP server registration, tool listing, and per-tool configuration overrides
- `tests` (10) for agent testing workflows and invocation management
- `pronunciation-dictionaries` (8) for dictionary CRUD and rule updates
- `conversations` (7) for ConvAI conversation access, audio retrieval, tokens, and feedback
- `text-to-speech` (6) for core synthesis and stream variants
- `tools` (6) for ConvAI tool lifecycle and dependency tracing
- `batch-calling` (6) for outbound call batch submit/retry/cancel lifecycle
- `whats-app` (6) for WhatsApp account management and outbound message/call triggers
- `history` (5) for generation record retrieval/download/deletion
- `music` (5) for composition, planning, and stem separation
- `phone-numbers` (5) for ConvAI number import/manage
- `service-accounts` (5) for machine principal and API key management
- `webhooks` (4) for workspace webhook lifecycle
- `speech-to-text` (4) for file conversion and realtime sessions
- `text-to-dialogue` (4) for dialogue generation (including streaming and timestamps)
- `text-to-voice` (4) for voice design/remix
- `audio-native` (3) for hosted audio-native experiences
- `audio-isolation` (2) for denoising/isolation
- `speech-to-speech` (2) for voice conversion
- `twilio` (2) for call registration and outbound telephony
- `user` (2) for profile and subscription metadata
- `widget` (2) for widget lifecycle
- `analytics`, `forced-alignment`, `llm-usage`, `models`, `sip-trunk`, `text-to-sound-effects`, `tokens`, `usage` (1 each)

Use this index to scope audits. A change in a high-volume family (`voices`, `studio`, `knowledge-base`, `agents`) warrants deeper regression review.

## Authentication and Environments
- Default auth: `xi-api-key` header on all REST calls.
- API keys support:
  - Scope restriction (endpoint-level access control)
  - Credit quota limits
- Never expose long-lived keys in browser/mobile clients.
- Use single-use tokens for client-side realtime flows:
  - Endpoint: `POST /v1/single-use-token/{token_type}`
  - `token_type` values: `realtime_scribe`, `tts_websocket`
  - Token lifetime: 15 minutes; consumed on use.
- Realtime regions (WS): `wss://api.elevenlabs.io`, `wss://api.us.elevenlabs.io`, `wss://api.eu.residency.elevenlabs.io`, `wss://api.in.residency.elevenlabs.io`

## API Family Selection
Use the simplest interface that satisfies latency and control requirements.

### Text to Speech (TTS)
- Batch synthesis: `POST /v1/text-to-speech/{voice_id}`
- HTTP streaming: `POST /v1/text-to-speech/{voice_id}/stream`
- WS low-latency incremental text: `GET /v1/text-to-speech/{voice_id}/stream-input`
- WS multi-context concurrency: `GET /v1/text-to-speech/{voice_id}/multi-stream-input`

Use HTTP when full text is available. Use WS when text arrives incrementally or alignment events are required.

### Speech to Text (STT)
- File/URL transcription: `POST /v1/speech-to-text`
- Realtime transcription: `GET /v1/speech-to-text/realtime`

Realtime STT message flow:
- Client sends `input_audio_chunk`
- Server emits `partial_transcript`, `committed_transcript`, or `committed_transcript_with_timestamps`
- Error payloads include typed `message_type` values such as `auth_error`, `rate_limited`, `quota_exceeded`

Commit strategy options: `manual` or `vad`.

### Speech-to-Speech and Audio Isolation
- Speech-to-speech conversion: `POST /v1/speech-to-speech/{voice_id}`
- Audio isolation conversion/streaming endpoints for denoising and source cleanup
- Prefer HTTP streaming when immediate playback matters and input is complete.

### Voices and Voice Design
- Discover and manage voices via the `voices` family.
- Use `text-to-voice` endpoints for design/remix workflows.
- Enforce deterministic voice IDs in config and avoid hard-coded IDs in application logic.

### Agents
- Core agent lifecycle: create, list, get, update, delete, duplicate
- Branching and drafts are first-class (`agents/branches/*`, `agents/drafts/*`)
- Simulation endpoints support pre-production validation
- Use tool/webhook configuration with strict schema checks and replay-safe handlers

### Webhooks
- Create: `POST /v1/workspace/webhooks`
- List: `GET /v1/workspace/webhooks`
- Update: `PATCH /v1/workspace/webhooks/{webhook_id}`
- Current create flow uses `auth_type: hmac`; persist returned `webhook_secret` immediately.
- Track operational health fields (`is_auto_disabled`, `most_recent_failure_error_code`, `most_recent_failure_timestamp`).

### Usage and Subscription Controls
- Usage metrics: `GET /v1/usage/character-stats`
- Subscription limits and capabilities: `GET /v1/user/subscription`
- Model catalog and request limits: `GET /v1/models`

## Advanced Family Notes

### Voices: modern and legacy overlap
- Voice discovery is currently centered on `GET /v2/voices` (`voices/search`), while several operations remain on `/v1/voices/*`.
- Core lifecycle references:
  - create IVC: `POST /v1/voices/add`
  - create PVC: `POST /v1/voices/pvc`
  - train PVC: `POST /v1/voices/pvc/{voice_id}/train`
  - edit metadata: `POST /v1/voices/{voice_id}/edit`
  - edit settings: `POST /v1/voices/{voice_id}/settings/edit`
- Always pin voice IDs and voice settings in config, not source constants spread across files.

### Studio: long-form generation pipeline
- Project lifecycle: `POST /v1/studio/projects`, `GET /v1/studio/projects`
- Conversion trigger: `POST /v1/studio/projects/{project_id}/convert`
- Snapshot access and streaming:
  - `GET /v1/studio/projects/{project_id}/snapshots`
  - `POST /v1/studio/projects/{project_id}/snapshots/{project_snapshot_id}/stream`
- Contributor rule: Studio changes must include at least one snapshot retrieval validation to catch post-convert regressions.

### Knowledge base and RAG
- Document ingestion: file, text, or URL under `/v1/convai/knowledge-base/*`
- Content retrieval: `GET /v1/convai/knowledge-base/{documentation_id}/content`
- RAG indexing is explicit, not implicit:
  - compute batch: `POST /v1/convai/knowledge-base/rag-index`
  - read status/result: `GET /v1/convai/knowledge-base/{documentation_id}/rag-index`
- Treat ingestion and indexing as separate states in app code; do not assume immediate queryability after upload.

### Agents, tools, tests, and MCP
- Agent core: `/v1/convai/agents*`
- Tool management: `/v1/convai/tools*`
- Test execution: `POST /v1/convai/agents/{agent_id}/run-tests`
- MCP integration:
  - register/list server: `/v1/convai/mcp-servers`
  - enumerate tools: `GET /v1/convai/mcp-servers/{mcp_server_id}/tools`
  - tool configuration overrides: `/v1/convai/mcp-servers/{mcp_server_id}/tool-configs`
- Design rule: store tool contracts in versioned schemas and validate agent-tool payloads before dispatch.

### Telephony and messaging connectors
- SIP: `POST /v1/convai/sip-trunk/outbound-call`
- Twilio: `POST /v1/convai/twilio/outbound-call`, `POST /v1/convai/twilio/register-call`
- WhatsApp: account operations plus outbound call/message in `/v1/convai/whatsapp*`
- Contributor rule: connector integrations require environment-specific e2e smoke checks and callback failure simulations.

## End-to-End Playbooks

### Playbook 1: Server-side TTS with observability
1. Resolve model and voice (`GET /v1/models`, voice endpoint as needed).
2. Generate audio via convert or stream endpoint.
3. Read `request-id` and `x-character-count` from headers.
4. Persist structured trace with endpoint/model/voice/latency/status.
5. If response is transiently unavailable (`429`/`5xx`), retry with jitter.
6. On repeated failure, degrade to simpler path (non-streaming fallback or queued processing).

### Playbook 2: Browser realtime TTS
1. Backend mints single-use token (`POST /v1/single-use-token/tts_websocket`).
2. Client opens `wss://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream-input` with `single_use_token`.
3. Client sends incremental text payloads.
4. Client processes audio and alignment events.
5. Client closes context/socket on idle timeout or UX completion.
6. Backend correlates session outcome with generated token metadata for abuse control.

### Playbook 3: Browser realtime STT
1. Backend mints single-use token (`realtime_scribe`).
2. Client opens `wss://api.elevenlabs.io/v1/speech-to-text/realtime?token=...`.
3. Client sends `input_audio_chunk` messages.
4. Client handles `partial_transcript`, `committed_transcript`, and timestamp variants.
5. Client handles typed error messages (`auth_error`, `rate_limited`, `quota_exceeded`).
6. System falls back to buffered upload transcription if realtime session cannot stabilize.

### Playbook 4: Dubbing pipeline
1. Submit dubbing job (`POST /v1/dubbing`).
2. Poll/list job state and transcript resources.
3. Pull dubbed audio/transcripts upon completion.
4. Apply language-specific QA checks (timing drift, truncation, speaker mismatch).
5. Store provenance: source media hash, target language, model, and generation timestamps.

### Playbook 5: Agent with KB and webhook callbacks
1. Ingest KB documents and compute RAG index.
2. Create/update agent with tool and webhook config.
3. Register workspace webhook (HMAC) and store secret securely.
4. Run simulation and tests pre-release.
5. Deploy with monitoring on tool call error payloads and webhook delivery failures.
6. Use branch workflows for iterative policy changes before merge.

## Error Taxonomy and Handling Contract
Treat all failures as typed events in logs and metrics.

- Auth failures:
  - Symptoms: HTTP `401`/`403`, realtime `auth_error`
  - Actions: rotate/reload credentials, verify scope/quota, stop retries
- Validation failures:
  - Symptoms: HTTP `422`
  - Actions: fix caller contract, add regression test, stop retries
- Capacity/rate failures:
  - Symptoms: HTTP `429`, realtime `rate_limited`, `quota_exceeded`, `resource_exhausted`
  - Actions: backoff, reduce concurrency, trigger budget or plan-limit alerts
- Transient service failures:
  - Symptoms: HTTP `5xx`, connection churn
  - Actions: exponential backoff with jitter, route failover if available
- Webhook delivery degradation:
  - Symptoms: `is_auto_disabled`, repeated non-200 callback outcomes
  - Actions: quarantine failing destination, replay queued events after fix

Minimum logging fields:
- `timestamp`
- `request_id` (if present)
- `endpoint`
- `operation`
- `status_or_message_type`
- `latency_ms`
- `retry_attempt`
- `correlation_id`

## Data and Schema Practices
- Keep generated API clients pinned by version and regenerate only in isolated PRs.
- Store internal DTOs separate from external API payloads.
- Validate outbound payloads before network calls.
- Fail closed on unknown enum values in safety-critical code paths.
- Add explicit schema translation tests when upstream adds fields.

## Release and Rollout Strategy
- Development:
  - use test keys and non-production webhook targets
  - verify header-level metadata collection early
- Staging:
  - run e2e for targeted family
  - test webhook signing and replay safety
  - test realtime reconnect behavior
- Production:
  - progressive rollout by traffic slice
  - alerting on error budget burn, latency spikes, and quota drift
  - rollback plan must include config-level endpoint path fallback

## Streaming and Realtime Standards
- HTTP streaming uses chunked transfer encoding and returns audio bytes incrementally.
- For realtime WS:
  - set explicit inactivity timeout handling client-side
  - process backpressure (do not assume unbounded consumer speed)
  - treat finalization messages explicitly (`isFinal`-style semantics for TTS WS)
  - correlate request/session IDs in logs
- In TTS WS, `single_use_token` query auth can replace `xi-api-key`.
- In STT realtime WS, auth can be `xi-api-key` header or `token` query parameter.

## Reliability Requirements
- Retry policy:
  - Retry on `429`, `500`, `502`, `503`, `504`
  - Do not blind-retry `4xx` auth/validation failures
  - Use exponential backoff with jitter
- Timeouts:
  - Set connect/read/write timeouts explicitly
  - Use shorter deadlines for interactive realtime paths than batch jobs
- Idempotency:
  - Implement client-side idempotency keys for any workflow that can be replayed
  - Persist outbound request fingerprints for at-least-once execution paths
- Degradation:
  - Fall back from WS to HTTP streaming or non-streaming when realtime channels fail
  - Surface partial results where safe

## Security and Compliance Guardrails
- Secrets:
  - Store API keys/webhook secrets in a secret manager, never in source
  - Rotate keys on schedule and after incident response
- Client-side safety:
  - Use single-use tokens for browser/mobile realtime use cases
  - Keep permanent keys server-side only
- Data retention:
  - Realtime STT supports `enable_logging=false`; docs note zero-retention mode is enterprise-only
- Transport:
  - HTTPS/WSS only
  - Reject non-TLS callback endpoints for webhooks

## Cost and Observability Standards
- Capture response headers where available:
  - `x-character-count`
  - `request-id`
- Maintain per-request telemetry:
  - endpoint, model, voice_id (if applicable), status code, latency, payload size
- Build periodic cost reports using `/v1/usage/character-stats` with:
  - `aggregation_interval` (`hour`, `day`, `week`, `month`, `cumulative`)
  - business-relevant breakdowns (`model`, `voice`, `user`, `region`, `request_source`, etc.)
- Enforce budget alerts and hard stop mechanisms when approaching subscription limits.

## Contributor Implementation Standards
When adding or changing ElevenLabs integrations in this repo, every PR must include:
1. Endpoint choice rationale (why this family/operation was selected)
2. Failure policy (retryable vs non-retryable errors)
3. Security posture (key handling, token flow, webhook auth)
4. Cost impact note (expected usage/cost delta)
5. Test evidence for success and failure paths

## Code Patterns
Use these as baseline integration idioms.

### cURL smoke test
```bash
curl 'https://api.elevenlabs.io/v1/models' \
  -H 'Content-Type: application/json' \
  -H "xi-api-key: $ELEVENLABS_API_KEY"
```

### TypeScript client bootstrap
```ts
import { ElevenLabsClient } from '@elevenlabs/elevenlabs-js';

export const elevenlabs = new ElevenLabsClient({
  apiKey: process.env.ELEVENLABS_API_KEY,
});
```

### Python client bootstrap
```py
from elevenlabs.client import ElevenLabs
import os

client = ElevenLabs(api_key=os.getenv("ELEVENLABS_API_KEY"))
```

## Testing and Acceptance
Minimum acceptance for production-facing changes:
- Unit tests for request construction and error mapping
- Integration test (or recorded fixture) for the target endpoint family
- Realtime tests for connect/send/receive/close lifecycle if WS is used
- Webhook signature verification and replay protection tests if webhooks are used
- Load or soak checks for high-concurrency or long-running streaming paths

## Maintenance Protocol
- Review changelog before each release train.
- Re-validate this file when new endpoint families or auth flows appear.
- Update examples when SDK major versions change.

Recent notable platform changes (February 2, 2026 changelog) include Eleven v3 stability/latency improvements, Text-to-Dialogue WAV output expansion, and multiple Agents/Workspace schema additions. Re-check assumptions against current docs before implementation.
