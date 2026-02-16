#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:-http://127.0.0.1:8080}"
INTERVAL_SEC="${INTERVAL_SEC:-2}"
SAMPLES="${SAMPLES:-0}" # 0 means infinite
TARGET_FIRST_TEXT_P95_MS="${TARGET_FIRST_TEXT_P95_MS:-550}"
TARGET_FIRST_AUDIO_P95_MS="${TARGET_FIRST_AUDIO_P95_MS:-1400}"
TARGET_TURN_TOTAL_P95_MS="${TARGET_TURN_TOTAL_P95_MS:-3200}"
TARGET_ASSISTANT_WORKING_P95_MS="${TARGET_ASSISTANT_WORKING_P95_MS:-650}"
FAIL_ON_TARGETS="${FAIL_ON_TARGETS:-0}"
REQUIRE_SAMPLES="${REQUIRE_SAMPLES:-0}"
MIN_STAGE_SAMPLES="${MIN_STAGE_SAMPLES:-1}"
FAIL_EARLY="${FAIL_EARLY:-1}"
RESET_WINDOW="${RESET_WINDOW:-0}"
REQUIRE_MIN_STAGE_SAMPLES="${REQUIRE_MIN_STAGE_SAMPLES:-0}"
LATEST_JSON=""

count=0
seen_samples=0
had_target_breach=0

echo "Latency probe -> ${BASE_URL}/v1/perf/latency (interval=${INTERVAL_SEC}s samples=${SAMPLES:-0})"
echo "Targets: assistant_working_p95<=${TARGET_ASSISTANT_WORKING_P95_MS}ms first_text_p95<=${TARGET_FIRST_TEXT_P95_MS}ms first_audio_p95<=${TARGET_FIRST_AUDIO_P95_MS}ms turn_total_p95<=${TARGET_TURN_TOTAL_P95_MS}ms"
echo "Fail on target breach: ${FAIL_ON_TARGETS}"
echo "Require measured samples: ${REQUIRE_SAMPLES}"
echo "Min stage samples before evaluating target: ${MIN_STAGE_SAMPLES}"
echo "Fail early on first breach: ${FAIL_EARLY}"
echo "Reset latency window before probe: ${RESET_WINDOW}"
echo "Require min stage samples by end: ${REQUIRE_MIN_STAGE_SAMPLES}"

if [[ "${RESET_WINDOW}" == "1" ]]; then
  if curl -fsS -X POST "${BASE_URL}/v1/perf/latency/reset" >/dev/null 2>&1; then
    echo "Latency window reset."
  else
    echo "warning: failed to reset latency window at ${BASE_URL}/v1/perf/latency/reset"
  fi
fi

while true; do
  now="$(date '+%H:%M:%S')"
  json="$(curl -fsS "${BASE_URL}/v1/perf/latency" || true)"
  if [[ -z "${json}" ]]; then
    echo "[${now}] fetch failed"
  else
    LATEST_JSON="${json}"
    has_samples="$(JSON_PAYLOAD="${json}" python3 - <<'PY'
import json, os
try:
    payload = json.loads(os.environ.get("JSON_PAYLOAD", "{}"))
except Exception:
    print("0")
    raise SystemExit(0)

found = False
for item in payload.get("stages", []):
    try:
        n = int(item.get("samples", 0) or 0)
    except Exception:
        n = 0
    if n > 0:
        found = True
        break
print("1" if found else "0")
PY
)"
    if [[ "${has_samples}" == "1" ]]; then
      seen_samples=1
    fi

    py_rc=0
    JSON_PAYLOAD="${json}" python3 - "$TARGET_FIRST_TEXT_P95_MS" "$TARGET_FIRST_AUDIO_P95_MS" "$TARGET_TURN_TOTAL_P95_MS" "$TARGET_ASSISTANT_WORKING_P95_MS" "$now" "$FAIL_ON_TARGETS" "$MIN_STAGE_SAMPLES" "$FAIL_EARLY" <<'PY' || py_rc=$?
import json
import os
import sys

target_text = float(sys.argv[1])
target_audio = float(sys.argv[2])
target_total = float(sys.argv[3])
target_working = float(sys.argv[4])
now = sys.argv[5]
fail_on_targets = str(sys.argv[6]).strip().lower() in ("1", "true", "yes", "on")
min_stage_samples = int(float(sys.argv[7]))
if min_stage_samples < 1:
    min_stage_samples = 1
fail_early = str(sys.argv[8]).strip().lower() in ("1", "true", "yes", "on")

try:
    payload = json.loads(os.environ.get("JSON_PAYLOAD", "{}"))
except Exception as exc:
    print(f"[{now}] invalid json: {exc}")
    raise SystemExit(0)

stages = {}
for item in payload.get("stages", []):
    stage = str(item.get("stage", ""))
    if stage:
        stages[stage] = item

def stage_eval(name, target):
    item = stages.get(name)
    if not item:
        return f"{name}: n/a", False
    p95 = float(item.get("p95_ms", 0) or 0)
    p50 = float(item.get("p50_ms", 0) or 0)
    samples = int(item.get("samples", 0) or 0)
    if samples <= 0:
        return f"{name}: n/a", False
    if samples < min_stage_samples:
        return f"{name}: p50={p50:.0f}ms p95={p95:.0f}ms n={samples} target={target:.0f} [warming]", False
    slow = p95 > target
    status = "ok" if not slow else "slow"
    return f"{name}: p50={p50:.0f}ms p95={p95:.0f}ms n={samples} target={target:.0f} [{status}]", slow

def stage_line(name):
    item = stages.get(name)
    if not item:
        return f"{name}: n/a"
    p95 = float(item.get("p95_ms", 0) or 0)
    p50 = float(item.get("p50_ms", 0) or 0)
    samples = int(item.get("samples", 0) or 0)
    if samples <= 0:
        return f"{name}: n/a"
    tag = "warming" if samples < min_stage_samples else "measured"
    return f"{name}: p50={p50:.0f}ms p95={p95:.0f}ms n={samples} [{tag}]"

line_working, fail_working = stage_eval("commit_to_assistant_working", target_working)
line_text, fail_text = stage_eval("commit_to_first_text", target_text)
line_audio, fail_audio = stage_eval("commit_to_first_audio", target_audio)
line_total, fail_total = stage_eval("turn_total", target_total)

extras = [
    stage_line("stop_to_stt_committed"),
    stage_line("commit_to_brain_first_delta"),
    stage_line("brain_first_delta_to_first_audio"),
]
print(
    f"[{now}] "
    + line_working
    + " | "
    + line_text
    + " | "
    + line_audio
    + " | "
    + line_total
    + " || "
    + " | ".join(extras)
)
if fail_on_targets and (fail_working or fail_text or fail_audio or fail_total):
    raise SystemExit(2 if fail_early else 11)
PY
    if [[ "${py_rc}" -ne 0 ]]; then
      if [[ "${py_rc}" -eq 11 ]]; then
        had_target_breach=1
      else
        exit "${py_rc}"
      fi
    fi
  fi

  count=$((count + 1))
  if [[ "${SAMPLES}" -gt 0 && "${count}" -ge "${SAMPLES}" ]]; then
    break
  fi
  sleep "${INTERVAL_SEC}"
done

if [[ "${REQUIRE_SAMPLES}" == "1" && "${seen_samples}" -eq 0 ]]; then
  echo "No measured latency samples were observed."
  echo "Speak to Samantha during the probe (at least a few complete turns), then rerun."
  exit 3
fi

if [[ "${REQUIRE_MIN_STAGE_SAMPLES}" == "1" ]]; then
  if [[ -z "${LATEST_JSON}" ]]; then
    echo "No latency snapshot data available at probe end."
    exit 4
  fi
  if ! JSON_PAYLOAD="${LATEST_JSON}" python3 - "$MIN_STAGE_SAMPLES" <<'PY'
import json
import os
import sys

min_samples = int(float(sys.argv[1]))
if min_samples < 1:
    min_samples = 1

try:
    payload = json.loads(os.environ.get("JSON_PAYLOAD", "{}"))
except Exception:
    print("Invalid final latency snapshot JSON.")
    raise SystemExit(4)

by_stage = {str(item.get("stage", "")): int(item.get("samples", 0) or 0) for item in payload.get("stages", [])}
required = ("commit_to_first_text", "commit_to_first_audio", "turn_total")
missing = []
for stage in required:
    n = by_stage.get(stage, 0)
    if n < min_samples:
        missing.append(f"{stage}: {n}/{min_samples}")

if missing:
    print("Insufficient stage samples by end of probe:")
    for item in missing:
        print(f"- {item}")
    raise SystemExit(4)
PY
  then
    exit $?
  fi
fi

if [[ "${FAIL_ON_TARGETS}" == "1" && "${had_target_breach}" -eq 1 ]]; then
  echo "Latency targets were breached during the probe window."
  exit 2
fi
