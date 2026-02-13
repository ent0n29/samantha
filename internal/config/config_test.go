package config

import (
	"strings"
	"testing"
	"time"
)

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
	if cfg.VoiceProvider != "local" {
		t.Fatalf("VoiceProvider = %q, want %q", cfg.VoiceProvider, "local")
	}
	if cfg.OpenClawHTTPURL != "" {
		t.Fatalf("OpenClawHTTPURL = %q, want empty default", cfg.OpenClawHTTPURL)
	}
	if cfg.OpenClawCLIThinking != "low" {
		t.Fatalf("OpenClawCLIThinking = %q, want %q", cfg.OpenClawCLIThinking, "low")
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

func TestLoadBackpressureAndRetentionDefaults(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9292")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StrictOutbound {
		t.Fatalf("StrictOutbound = true, want false")
	}
	if !cfg.UIAudioWorklet {
		t.Fatalf("UIAudioWorklet = false, want true")
	}
	if cfg.WSBackpressureMode != "drop" {
		t.Fatalf("WSBackpressureMode = %q, want %q", cfg.WSBackpressureMode, "drop")
	}
	if cfg.SessionRetention != 24*time.Hour {
		t.Fatalf("SessionRetention = %s, want 24h", cfg.SessionRetention)
	}
	if cfg.TaskRuntimeEnabled {
		t.Fatalf("TaskRuntimeEnabled = true, want false")
	}
	if cfg.TaskTimeout != 20*time.Minute {
		t.Fatalf("TaskTimeout = %s, want 20m", cfg.TaskTimeout)
	}
	if cfg.TaskIdempotencyWindow != 10*time.Second {
		t.Fatalf("TaskIdempotencyWindow = %s, want 10s", cfg.TaskIdempotencyWindow)
	}
	if cfg.OpenClawHTTPStreamStrict {
		t.Fatalf("OpenClawHTTPStreamStrict = true, want false")
	}
}

func TestLoadRejectsInvalidBackpressureMode(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9393")
	t.Setenv("APP_WS_BACKPRESSURE_MODE", "foo")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid APP_WS_BACKPRESSURE_MODE")
	}
	if !strings.Contains(err.Error(), "APP_WS_BACKPRESSURE_MODE") {
		t.Fatalf("Load() error = %v, want backpressure mode parse/validation error", err)
	}
}

func TestLoadRejectsInvalidOpenClawCLIThinking(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9394")
	t.Setenv("OPENCLAW_CLI_THINKING", "ultra")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid OPENCLAW_CLI_THINKING")
	}
	if !strings.Contains(err.Error(), "OPENCLAW_CLI_THINKING") {
		t.Fatalf("Load() error = %v, want OPENCLAW_CLI_THINKING validation error", err)
	}
}

func TestLoadParsesSessionRetention(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9494")
	t.Setenv("APP_SESSION_RETENTION", "36h")
	t.Setenv("APP_STRICT_OUTBOUND", "true")
	t.Setenv("APP_UI_AUDIO_WORKLET", "false")
	t.Setenv("APP_WS_BACKPRESSURE_MODE", "block")
	t.Setenv("APP_TASK_RUNTIME_ENABLED", "true")
	t.Setenv("APP_TASK_TIMEOUT", "30m")
	t.Setenv("APP_TASK_IDEMPOTENCY_WINDOW", "15s")
	t.Setenv("OPENCLAW_HTTP_STREAM_STRICT", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SessionRetention != 36*time.Hour {
		t.Fatalf("SessionRetention = %s, want 36h", cfg.SessionRetention)
	}
	if !cfg.StrictOutbound {
		t.Fatalf("StrictOutbound = false, want true")
	}
	if cfg.UIAudioWorklet {
		t.Fatalf("UIAudioWorklet = true, want false")
	}
	if cfg.WSBackpressureMode != "block" {
		t.Fatalf("WSBackpressureMode = %q, want %q", cfg.WSBackpressureMode, "block")
	}
	if !cfg.TaskRuntimeEnabled {
		t.Fatalf("TaskRuntimeEnabled = false, want true")
	}
	if cfg.TaskTimeout != 30*time.Minute {
		t.Fatalf("TaskTimeout = %s, want 30m", cfg.TaskTimeout)
	}
	if cfg.TaskIdempotencyWindow != 15*time.Second {
		t.Fatalf("TaskIdempotencyWindow = %s, want 15s", cfg.TaskIdempotencyWindow)
	}
	if !cfg.OpenClawHTTPStreamStrict {
		t.Fatalf("OpenClawHTTPStreamStrict = false, want true")
	}
}

func setCoreEnvEmpty(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_BIND_ADDR",
		"APP_SHUTDOWN_TIMEOUT",
		"APP_SESSION_INACTIVITY_TIMEOUT",
		"APP_SESSION_RETENTION",
		"APP_FIRST_AUDIO_SLO",
		"APP_TASK_RUNTIME_ENABLED",
		"APP_TASK_TIMEOUT",
		"APP_TASK_IDEMPOTENCY_WINDOW",
		"APP_METRICS_NAMESPACE",
		"APP_ALLOW_ANY_ORIGIN",
		"APP_STRICT_OUTBOUND",
		"APP_UI_AUDIO_WORKLET",
		"APP_WS_BACKPRESSURE_MODE",
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
		"OPENCLAW_CLI_THINKING",
		"OPENCLAW_HTTP_STREAM_STRICT",
		"DATABASE_URL",
		"MEMORY_EMBEDDING_DIM",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
