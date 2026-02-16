#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${1:-http://127.0.0.1:8080}"

TURNS="${PERF_REPLAY_TURNS:-10}"
CHUNK_MS="${PERF_REPLAY_CHUNK_MS:-45}"
REALTIME="${PERF_REPLAY_REALTIME:-3.0}"
START_DELAY_MS="${PERF_REPLAY_START_DELAY_MS:-900}"
INTER_TURN_MS="${PERF_REPLAY_INTER_TURN_MS:-180}"
TURN_TIMEOUT_MS="${PERF_REPLAY_TURN_TIMEOUT_MS:-15000}"
USER_ID="${PERF_REPLAY_USER_ID:-perf-replay}"
PERSONA_ID="${PERF_REPLAY_PERSONA_ID:-concise}"
VOICE_ID="${PERF_REPLAY_VOICE_ID:-}"
MODEL_ID="${PERF_REPLAY_MODEL_ID:-}"
TEXTS="${PERF_REPLAY_TEXTS:-}"
VERBOSE="${PERF_REPLAY_VERBOSE:-1}"

ARM64_GO="${HOME}/.local/arm64/go/bin/go"
if [[ -x "${ARM64_GO}" ]]; then
  GO_CMD=(arch -arm64 "${ARM64_GO}")
else
  GO_CMD=(go)
fi

ARGS=(
  run ./cmd/perfvoice
  -base-url "${BASE_URL}"
  -user-id "${USER_ID}"
  -persona-id "${PERSONA_ID}"
  -turns "${TURNS}"
  -chunk-ms "${CHUNK_MS}"
  -realtime "${REALTIME}"
  -start-delay-ms "${START_DELAY_MS}"
  -inter-turn-ms "${INTER_TURN_MS}"
  -turn-timeout-ms "${TURN_TIMEOUT_MS}"
)

if [[ "${VERBOSE}" == "0" || "${VERBOSE}" == "false" ]]; then
  ARGS+=(-verbose=false)
fi
if [[ -n "${VOICE_ID}" ]]; then
  ARGS+=(-voice-id "${VOICE_ID}")
fi
if [[ -n "${MODEL_ID}" ]]; then
  ARGS+=(-model-id "${MODEL_ID}")
fi
if [[ -n "${TEXTS}" ]]; then
  ARGS+=(-texts "${TEXTS}")
fi

(
  cd "${ROOT}"
  "${GO_CMD[@]}" "${ARGS[@]}"
)
