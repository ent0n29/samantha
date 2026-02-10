package config

import "testing"

func TestLoadDefaultsDoNotSetOpenClawHTTPURL(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.OpenClawAdapterMode != "auto" {
		t.Fatalf("OpenClawAdapterMode = %q, want %q", cfg.OpenClawAdapterMode, "auto")
	}
	if cfg.OpenClawHTTPURL != "" {
		t.Fatalf("OpenClawHTTPURL = %q, want empty default", cfg.OpenClawHTTPURL)
	}
}

func TestLoadUsesExplicitOpenClawHTTPURL(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9191")
	t.Setenv("OPENCLAW_HTTP_URL", "http://localhost:7777/custom")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OpenClawHTTPURL != "http://localhost:7777/custom" {
		t.Fatalf("OpenClawHTTPURL = %q, want explicit value", cfg.OpenClawHTTPURL)
	}
}

func setCoreEnvEmpty(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_BIND_ADDR",
		"APP_SHUTDOWN_TIMEOUT",
		"APP_SESSION_INACTIVITY_TIMEOUT",
		"APP_FIRST_AUDIO_SLO",
		"APP_METRICS_NAMESPACE",
		"APP_ALLOW_ANY_ORIGIN",
		"VOICE_PROVIDER",
		"ELEVENLABS_API_KEY",
		"ELEVENLABS_WS_BASE_URL",
		"ELEVENLABS_TTS_VOICE_ID",
		"ELEVENLABS_TTS_MODEL_ID",
		"ELEVENLABS_STT_MODEL_ID",
		"ELEVENLABS_TTS_OUTPUT_FORMAT",
		"ELEVENLABS_STT_COMMIT_STRATEGY",
		"LOCAL_WHISPER_CLI",
		"LOCAL_WHISPER_MODEL_PATH",
		"LOCAL_WHISPER_LANGUAGE",
		"LOCAL_WHISPER_THREADS",
		"LOCAL_WHISPER_BEAM_SIZE",
		"LOCAL_WHISPER_BEST_OF",
		"LOCAL_KOKORO_PYTHON",
		"LOCAL_KOKORO_WORKER_SCRIPT",
		"LOCAL_KOKORO_VOICE",
		"LOCAL_KOKORO_LANG_CODE",
		"OPENCLAW_ADAPTER_MODE",
		"OPENCLAW_HTTP_URL",
		"OPENCLAW_CLI_PATH",
		"DATABASE_URL",
		"MEMORY_EMBEDDING_DIM",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
