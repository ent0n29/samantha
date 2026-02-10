#!/usr/bin/env python3
"""
Persistent Kokoro TTS worker.

Protocol: JSON lines over stdin/stdout.
Request:
  {"id":"...","text":"...","voice":"af_heart","speed":1.0,"lang_code":"a"}
Response:
  {"id":"...","ok":true,"format":"wav_24000","sample_rate":24000,"audio_base64":"..."}
  {"id":"...","ok":false,"error":"..."}
"""

from __future__ import annotations

import base64
import io
import json
import os
import sys
import traceback
import wave


def eprint(*args: object) -> None:
    print(*args, file=sys.stderr, flush=True)


def wav_bytes_from_float32_mono(audio, sample_rate: int) -> bytes:
    """Encode float32 mono audio in [-1, 1] to PCM16 WAV bytes.

    We avoid `soundfile` (libsndfile) to keep local setup friction low.
    """

    import numpy as np  # type: ignore

    if sample_rate <= 0:
        sample_rate = 24000

    a = np.asarray(audio, dtype=np.float32).reshape((-1,))
    if a.size == 0:
        pcm = b""
    else:
        a = np.clip(a, -1.0, 1.0)
        pcm = (a * 32767.0).astype(np.int16).tobytes()

    buf = io.BytesIO()
    with wave.open(buf, "wb") as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)  # PCM16
        wf.setframerate(int(sample_rate))
        wf.writeframes(pcm)
    return buf.getvalue()


def main() -> int:
    # Keep stdout strictly for JSON responses. Many ML deps print warnings/progress to stdout,
    # so we redirect process-level stdout to stderr and write JSON to the original FD.
    json_fd = os.dup(1)
    try:
        os.dup2(2, 1)
    except Exception:
        # Best effort; fall back to normal stdout if dup2 fails.
        pass
    json_out = os.fdopen(json_fd, "w", buffering=1)

    try:
        from kokoro import KPipeline  # type: ignore
        import numpy as np  # type: ignore
    except Exception as exc:
        eprint("kokoro worker import failed:", exc)
        return 1

    # Allow callers to override this, but keep it sane for our use-case.
    # Kokoro's README suggests setting PYTORCH_ENABLE_MPS_FALLBACK=1 on Apple Silicon.
    os.environ.setdefault("PYTORCH_ENABLE_MPS_FALLBACK", "1")

    # Default pipeline; requests can override lang_code.
    pipelines: dict[str, KPipeline] = {}

    def get_pipeline(lang_code: str) -> KPipeline:
        lang_code = (lang_code or "a").strip() or "a"
        p = pipelines.get(lang_code)
        if p is None:
            # Suppress the default repo_id warning by passing it explicitly if supported.
            try:
                p = KPipeline(lang_code=lang_code, repo_id="hexgrad/Kokoro-82M")
            except TypeError:
                p = KPipeline(lang_code=lang_code)
            pipelines[lang_code] = p
        return p

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        rid = ""
        try:
            req = json.loads(line)
            rid = str(req.get("id") or "")
            text = str(req.get("text") or "").strip()
            voice = str(req.get("voice") or "af_heart").strip() or "af_heart"
            lang_code = str(req.get("lang_code") or "a").strip() or "a"
            speed = float(req.get("speed") or 1.0)

            # Keep speed in a sensible range.
            if speed < 0.7:
                speed = 0.7
            if speed > 1.2:
                speed = 1.2

            if not text:
                resp = {"id": rid, "ok": True, "format": "wav_24000", "sample_rate": 24000, "audio_base64": ""}
                json_out.write(json.dumps(resp) + "\n")
                json_out.flush()
                continue

            pipeline = get_pipeline(lang_code)
            audio_parts: list[np.ndarray] = []
            # Split on newlines by default; caller can pre-format text with line breaks.
            generator = pipeline(text, voice=voice, speed=speed, split_pattern=r"\n+")
            for _, _, audio in generator:
                if audio is None:
                    continue
                audio_parts.append(audio)

            if not audio_parts:
                audio_all = np.zeros((0,), dtype=np.float32)
            else:
                audio_all = np.concatenate(audio_parts)

            # Kokoro outputs 24kHz float audio. Encode as PCM16 WAV for browser playback.
            wav_bytes = wav_bytes_from_float32_mono(audio_all, 24000)
            resp = {
                "id": rid,
                "ok": True,
                "format": "wav_24000",
                "sample_rate": 24000,
                "audio_base64": base64.b64encode(wav_bytes).decode("ascii"),
            }
        except Exception as exc:
            eprint("kokoro worker error:", exc)
            eprint(traceback.format_exc())
            resp = {"id": rid, "ok": False, "error": str(exc)}

        json_out.write(json.dumps(resp) + "\n")
        json_out.flush()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
