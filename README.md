# Samantha

Minimal, voice-first companion (local-first, Apple Silicon friendly).

## Quickstart (macOS)

```bash
make dev
```

Open:

- `http://127.0.0.1:8080/ui/` (add `?onboarding=1` to rerun first-run checks)

## Voice Backends

- Local (default): Whisper.cpp STT + Kokoro TTS (`VOICE_PROVIDER=local`)
- ElevenLabs (optional): set `VOICE_PROVIDER=elevenlabs` + `ELEVENLABS_API_KEY`

See `.env.example` for configuration.
