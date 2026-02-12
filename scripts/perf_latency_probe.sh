#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:-http://127.0.0.1:8080}"
INTERVAL_SEC="${INTERVAL_SEC:-2}"
SAMPLES="${SAMPLES:-0}" # 0 means infinite
TARGET_FIRST_TEXT_P95_MS="${TARGET_FIRST_TEXT_P95_MS:-400}"
TARGET_FIRST_AUDIO_P95_MS="${TARGET_FIRST_AUDIO_P95_MS:-900}"
TARGET_TURN_TOTAL_P95_MS="${TARGET_TURN_TOTAL_P95_MS:-3500}"

count=0

echo "Latency probe -> ${BASE_URL}/v1/perf/latency (interval=${INTERVAL_SEC}s samples=${SAMPLES:-0})"
echo "Targets: first_text_p95<=${TARGET_FIRST_TEXT_P95_MS}ms first_audio_p95<=${TARGET_FIRST_AUDIO_P95_MS}ms turn_total_p95<=${TARGET_TURN_TOTAL_P95_MS}ms"

while true; do
  now="$(date '+%H:%M:%S')"
  json="$(curl -fsS "${BASE_URL}/v1/perf/latency" || true)"
  if [[ -z "${json}" ]]; then
    echo "[${now}] fetch failed"
  else
    JSON_PAYLOAD="${json}" python3 - "$TARGET_FIRST_TEXT_P95_MS" "$TARGET_FIRST_AUDIO_P95_MS" "$TARGET_TURN_TOTAL_P95_MS" "$now" <<'PY'
import json
import os
import sys

target_text = float(sys.argv[1])
target_audio = float(sys.argv[2])
target_total = float(sys.argv[3])
now = sys.argv[4]

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

def stage_line(name, target):
    item = stages.get(name)
    if not item:
        return f"{name}: n/a"
    p95 = float(item.get("p95_ms", 0) or 0)
    p50 = float(item.get("p50_ms", 0) or 0)
    samples = int(item.get("samples", 0) or 0)
    status = "ok" if p95 <= target else "slow"
    return f"{name}: p50={p50:.0f}ms p95={p95:.0f}ms n={samples} target={target:.0f} [{status}]"

print(
    f"[{now}] "
    + stage_line("commit_to_first_text", target_text)
    + " | "
    + stage_line("commit_to_first_audio", target_audio)
    + " | "
    + stage_line("turn_total", target_total)
)
PY
  fi

  count=$((count + 1))
  if [[ "${SAMPLES}" -gt 0 && "${count}" -ge "${SAMPLES}" ]]; then
    break
  fi
  sleep "${INTERVAL_SEC}"
done
