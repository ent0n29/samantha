#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${1:-http://127.0.0.1:8080}"

INTERVAL_SEC="${INTERVAL_SEC:-1}"
SAMPLES="${SAMPLES:-30}"
TARGET_FIRST_TEXT_P95_MS="${TARGET_FIRST_TEXT_P95_MS:-550}"
TARGET_FIRST_AUDIO_P95_MS="${TARGET_FIRST_AUDIO_P95_MS:-1400}"
TARGET_TURN_TOTAL_P95_MS="${TARGET_TURN_TOTAL_P95_MS:-3200}"
FAIL_ON_TARGETS="${FAIL_ON_TARGETS:-1}"
REQUIRE_LOCAL_PROVIDER="${REQUIRE_LOCAL_PROVIDER:-0}"
REQUIRE_SAMPLES="${REQUIRE_SAMPLES:-1}"
MIN_STAGE_SAMPLES="${MIN_STAGE_SAMPLES:-5}"
FAIL_EARLY="${FAIL_EARLY:-0}"
RESET_WINDOW="${RESET_WINDOW:-1}"
REQUIRE_MIN_STAGE_SAMPLES="${REQUIRE_MIN_STAGE_SAMPLES:-1}"

echo "Local-first latency baseline (VOICE_PROVIDER=local expected)"
echo "interval=${INTERVAL_SEC}s samples=${SAMPLES} fail_on_targets=${FAIL_ON_TARGETS} require_samples=${REQUIRE_SAMPLES} min_stage_samples=${MIN_STAGE_SAMPLES} fail_early=${FAIL_EARLY} reset_window=${RESET_WINDOW}"
echo "Tip: talk to Samantha while this runs so latency stages are populated."

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

INTERVAL_SEC="${INTERVAL_SEC}" \
SAMPLES="${SAMPLES}" \
TARGET_FIRST_TEXT_P95_MS="${TARGET_FIRST_TEXT_P95_MS}" \
TARGET_FIRST_AUDIO_P95_MS="${TARGET_FIRST_AUDIO_P95_MS}" \
TARGET_TURN_TOTAL_P95_MS="${TARGET_TURN_TOTAL_P95_MS}" \
FAIL_ON_TARGETS="${FAIL_ON_TARGETS}" \
REQUIRE_SAMPLES="${REQUIRE_SAMPLES}" \
MIN_STAGE_SAMPLES="${MIN_STAGE_SAMPLES}" \
FAIL_EARLY="${FAIL_EARLY}" \
RESET_WINDOW="${RESET_WINDOW}" \
REQUIRE_MIN_STAGE_SAMPLES="${REQUIRE_MIN_STAGE_SAMPLES}" \
  "${ROOT}/scripts/perf_latency_probe.sh" "${BASE_URL}"
