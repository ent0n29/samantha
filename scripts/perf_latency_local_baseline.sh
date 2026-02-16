#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${1:-http://127.0.0.1:8080}"

INTERVAL_SEC="${INTERVAL_SEC:-1}"
SAMPLES="${SAMPLES:-30}"
TARGET_FIRST_TEXT_P95_MS="${TARGET_FIRST_TEXT_P95_MS:-550}"
TARGET_FIRST_AUDIO_P95_MS="${TARGET_FIRST_AUDIO_P95_MS:-1400}"
TARGET_TURN_TOTAL_P95_MS="${TARGET_TURN_TOTAL_P95_MS:-3200}"
TARGET_ASSISTANT_WORKING_P95_MS="${TARGET_ASSISTANT_WORKING_P95_MS:-650}"
FAIL_ON_TARGETS="${FAIL_ON_TARGETS:-1}"
REQUIRE_LOCAL_PROVIDER="${REQUIRE_LOCAL_PROVIDER:-0}"
REQUIRE_SAMPLES="${REQUIRE_SAMPLES:-1}"
MIN_STAGE_SAMPLES="${MIN_STAGE_SAMPLES:-5}"
FAIL_EARLY="${FAIL_EARLY:-0}"
RESET_WINDOW="${RESET_WINDOW:-1}"
REQUIRE_MIN_STAGE_SAMPLES="${REQUIRE_MIN_STAGE_SAMPLES:-1}"
AUTO_REPLAY="${AUTO_REPLAY:-1}"
AUTO_REPLAY_STRICT="${AUTO_REPLAY_STRICT:-1}"
AUTO_REPLAY_TURNS="${AUTO_REPLAY_TURNS:-10}"
AUTO_REPLAY_CHUNK_MS="${AUTO_REPLAY_CHUNK_MS:-45}"
AUTO_REPLAY_REALTIME="${AUTO_REPLAY_REALTIME:-3.0}"
AUTO_REPLAY_START_DELAY_MS="${AUTO_REPLAY_START_DELAY_MS:-900}"
AUTO_REPLAY_INTER_TURN_MS="${AUTO_REPLAY_INTER_TURN_MS:-180}"
AUTO_REPLAY_TURN_TIMEOUT_MS="${AUTO_REPLAY_TURN_TIMEOUT_MS:-15000}"

echo "Local-first latency baseline (VOICE_PROVIDER=local expected)"
echo "interval=${INTERVAL_SEC}s samples=${SAMPLES} fail_on_targets=${FAIL_ON_TARGETS} require_samples=${REQUIRE_SAMPLES} min_stage_samples=${MIN_STAGE_SAMPLES} fail_early=${FAIL_EARLY} reset_window=${RESET_WINDOW}"
if [[ "${AUTO_REPLAY}" == "1" ]]; then
  echo "Auto replay: enabled (turns=${AUTO_REPLAY_TURNS}, chunk_ms=${AUTO_REPLAY_CHUNK_MS}, realtime=${AUTO_REPLAY_REALTIME})"
else
  echo "Tip: talk to Samantha while this runs so latency stages are populated."
fi

onboarding_json="$(curl -fsS "${BASE_URL%/}/v1/onboarding/status" || true)"
if [[ -n "${onboarding_json}" ]]; then
  provider="$(JSON_PAYLOAD="${onboarding_json}" python3 - <<'PY'
import json, os
try:
    payload = json.loads(os.environ.get("JSON_PAYLOAD", "{}"))
except Exception:
    print("")
    raise SystemExit(0)
print(str(payload.get("voice_provider", "")).strip().lower())
PY
)"
  if [[ -n "${provider}" && "${provider}" != "local" ]]; then
    echo "warning: onboarding reports voice_provider=${provider}, not local"
    if [[ "${REQUIRE_LOCAL_PROVIDER}" == "1" ]]; then
      echo "error: REQUIRE_LOCAL_PROVIDER=1 and provider is not local"
      exit 1
    fi
  fi
fi

replay_pid=""
replay_rc=0
probe_rc=0

cleanup() {
  if [[ -n "${replay_pid}" ]]; then
    if kill -0 "${replay_pid}" >/dev/null 2>&1; then
      kill "${replay_pid}" >/dev/null 2>&1 || true
    fi
  fi
}
trap cleanup EXIT

if [[ "${AUTO_REPLAY}" == "1" ]]; then
  PERF_REPLAY_TURNS="${AUTO_REPLAY_TURNS}" \
  PERF_REPLAY_CHUNK_MS="${AUTO_REPLAY_CHUNK_MS}" \
  PERF_REPLAY_REALTIME="${AUTO_REPLAY_REALTIME}" \
  PERF_REPLAY_START_DELAY_MS="${AUTO_REPLAY_START_DELAY_MS}" \
  PERF_REPLAY_INTER_TURN_MS="${AUTO_REPLAY_INTER_TURN_MS}" \
  PERF_REPLAY_TURN_TIMEOUT_MS="${AUTO_REPLAY_TURN_TIMEOUT_MS}" \
  PERF_REPLAY_VERBOSE="${PERF_REPLAY_VERBOSE:-1}" \
    "${ROOT}/scripts/perf_voice_replay.sh" "${BASE_URL}" &
  replay_pid=$!
fi

INTERVAL_SEC="${INTERVAL_SEC}" \
SAMPLES="${SAMPLES}" \
TARGET_FIRST_TEXT_P95_MS="${TARGET_FIRST_TEXT_P95_MS}" \
TARGET_FIRST_AUDIO_P95_MS="${TARGET_FIRST_AUDIO_P95_MS}" \
TARGET_TURN_TOTAL_P95_MS="${TARGET_TURN_TOTAL_P95_MS}" \
TARGET_ASSISTANT_WORKING_P95_MS="${TARGET_ASSISTANT_WORKING_P95_MS}" \
FAIL_ON_TARGETS="${FAIL_ON_TARGETS}" \
REQUIRE_SAMPLES="${REQUIRE_SAMPLES}" \
MIN_STAGE_SAMPLES="${MIN_STAGE_SAMPLES}" \
FAIL_EARLY="${FAIL_EARLY}" \
RESET_WINDOW="${RESET_WINDOW}" \
REQUIRE_MIN_STAGE_SAMPLES="${REQUIRE_MIN_STAGE_SAMPLES}" \
  "${ROOT}/scripts/perf_latency_probe.sh" "${BASE_URL}" || probe_rc=$?

if [[ -n "${replay_pid}" ]]; then
  if kill -0 "${replay_pid}" >/dev/null 2>&1; then
    # Probe window is complete; stop replay so the benchmark exits promptly.
    kill "${replay_pid}" >/dev/null 2>&1 || true
    wait "${replay_pid}" >/dev/null 2>&1 || true
  else
    wait "${replay_pid}" || replay_rc=$?
    if [[ "${replay_rc}" -ne 0 ]]; then
      echo "warning: auto replay exited with code ${replay_rc}"
      if [[ "${AUTO_REPLAY_STRICT}" == "1" && "${probe_rc}" -eq 0 ]]; then
        probe_rc="${replay_rc}"
      fi
    fi
  fi
fi

exit "${probe_rc}"
