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
	if cfg.OpenClawCLIThinking != "minimal" {
		t.Fatalf("OpenClawCLIThinking = %q, want %q", cfg.OpenClawCLIThinking, "minimal")
	}
	if !cfg.OpenClawCLIStreaming {
		t.Fatalf("OpenClawCLIStreaming = false, want true")
	}
	if cfg.OpenClawCLIStreamMinChars != 16 {
		t.Fatalf("OpenClawCLIStreamMinChars = %d, want %d", cfg.OpenClawCLIStreamMinChars, 16)
	}
	if cfg.LocalSTTProfile != "balanced" {
		t.Fatalf("LocalSTTProfile = %q, want %q", cfg.LocalSTTProfile, "balanced")
	}
	if !cfg.LocalSTTAutoDownload {
		t.Fatalf("LocalSTTAutoDownload = false, want true")
	}
	if cfg.LocalWhisperModelPath != ".models/whisper/ggml-base.en.bin" {
		t.Fatalf("LocalWhisperModelPath = %q, want %q", cfg.LocalWhisperModelPath, ".models/whisper/ggml-base.en.bin")
	}
	if cfg.LocalWhisperBeamSize != 2 {
		t.Fatalf("LocalWhisperBeamSize = %d, want %d", cfg.LocalWhisperBeamSize, 2)
	}
	if cfg.LocalWhisperBestOf != 2 {
		t.Fatalf("LocalWhisperBestOf = %d, want %d", cfg.LocalWhisperBestOf, 2)
	}
	if cfg.AssistantWorkingDelay != 500*time.Millisecond {
		t.Fatalf("AssistantWorkingDelay = %s, want 500ms", cfg.AssistantWorkingDelay)
	}
	if cfg.UISilenceBreakerMode != "visual" {
		t.Fatalf("UISilenceBreakerMode = %q, want %q", cfg.UISilenceBreakerMode, "visual")
	}
	if cfg.UISilenceBreakerDelay != 750*time.Millisecond {
		t.Fatalf("UISilenceBreakerDelay = %s, want 750ms", cfg.UISilenceBreakerDelay)
	}
	if cfg.UIVADMinUtterance != 650*time.Millisecond {
		t.Fatalf("UIVADMinUtterance = %s, want 650ms", cfg.UIVADMinUtterance)
	}
	if cfg.UIVADGrace != 220*time.Millisecond {
		t.Fatalf("UIVADGrace = %s, want 220ms", cfg.UIVADGrace)
	}
	if cfg.UIAudioSegmentOverlap != 22*time.Millisecond {
		t.Fatalf("UIAudioSegmentOverlap = %s, want 22ms", cfg.UIAudioSegmentOverlap)
	}
	if cfg.UIFillerMode != "adaptive" {
		t.Fatalf("UIFillerMode = %q, want %q", cfg.UIFillerMode, "adaptive")
	}
	if cfg.UIFillerMinDelay != 1200*time.Millisecond {
		t.Fatalf("UIFillerMinDelay = %s, want 1200ms", cfg.UIFillerMinDelay)
	}
	if cfg.UIFillerCooldown != 18*time.Second {
		t.Fatalf("UIFillerCooldown = %s, want 18s", cfg.UIFillerCooldown)
	}
	if cfg.UIFillerMaxPerTurn != 1 {
		t.Fatalf("UIFillerMaxPerTurn = %d, want 1", cfg.UIFillerMaxPerTurn)
	}
	if cfg.UITaskDeskDefault {
		t.Fatalf("UITaskDeskDefault = true, want false")
	}
	if cfg.UIVADProfile != "default" {
		t.Fatalf("UIVADProfile = %q, want %q", cfg.UIVADProfile, "default")
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

func TestLoadRejectsInvalidUISilenceBreakerMode(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9399")
	t.Setenv("APP_UI_SILENCE_BREAKER_MODE", "noisy")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid APP_UI_SILENCE_BREAKER_MODE")
	}
	if !strings.Contains(err.Error(), "APP_UI_SILENCE_BREAKER_MODE") {
		t.Fatalf("Load() error = %v, want APP_UI_SILENCE_BREAKER_MODE validation error", err)
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

func TestLoadRejectsInvalidOpenClawCLIStreamMinChars(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9395")
	t.Setenv("OPENCLAW_CLI_STREAM_MIN_CHARS", "0")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid OPENCLAW_CLI_STREAM_MIN_CHARS")
	}
	if !strings.Contains(err.Error(), "OPENCLAW_CLI_STREAM_MIN_CHARS") {
		t.Fatalf("Load() error = %v, want OPENCLAW_CLI_STREAM_MIN_CHARS validation error", err)
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
	t.Setenv("APP_ASSISTANT_WORKING_DELAY", "320ms")
	t.Setenv("APP_UI_SILENCE_BREAKER_MODE", "off")
	t.Setenv("APP_UI_SILENCE_BREAKER_DELAY", "1400ms")
	t.Setenv("APP_UI_VAD_MIN_UTTERANCE", "800ms")
	t.Setenv("APP_UI_VAD_GRACE", "260ms")
	t.Setenv("APP_UI_AUDIO_SEGMENT_OVERLAP", "30ms")
	t.Setenv("APP_FILLER_MODE", "off")
	t.Setenv("APP_FILLER_MIN_DELAY", "1600ms")
	t.Setenv("APP_FILLER_COOLDOWN", "27s")
	t.Setenv("APP_FILLER_MAX_PER_TURN", "2")
	t.Setenv("APP_LOCAL_STT_PROFILE", "fast")
	t.Setenv("APP_LOCAL_STT_AUTO_MODEL_DOWNLOAD", "false")
	t.Setenv("APP_UI_TASK_DESK_DEFAULT", "true")
	t.Setenv("APP_UI_VAD_PROFILE", "default")
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
	if cfg.AssistantWorkingDelay != 320*time.Millisecond {
		t.Fatalf("AssistantWorkingDelay = %s, want 320ms", cfg.AssistantWorkingDelay)
	}
	if cfg.UISilenceBreakerMode != "off" {
		t.Fatalf("UISilenceBreakerMode = %q, want %q", cfg.UISilenceBreakerMode, "off")
	}
	if cfg.UISilenceBreakerDelay != 1400*time.Millisecond {
		t.Fatalf("UISilenceBreakerDelay = %s, want 1400ms", cfg.UISilenceBreakerDelay)
	}
	if cfg.UIVADMinUtterance != 800*time.Millisecond {
		t.Fatalf("UIVADMinUtterance = %s, want 800ms", cfg.UIVADMinUtterance)
	}
	if cfg.UIVADGrace != 260*time.Millisecond {
		t.Fatalf("UIVADGrace = %s, want 260ms", cfg.UIVADGrace)
	}
	if cfg.UIAudioSegmentOverlap != 30*time.Millisecond {
		t.Fatalf("UIAudioSegmentOverlap = %s, want 30ms", cfg.UIAudioSegmentOverlap)
	}
	if cfg.UIFillerMode != "off" {
		t.Fatalf("UIFillerMode = %q, want %q", cfg.UIFillerMode, "off")
	}
	if cfg.UIFillerMinDelay != 1600*time.Millisecond {
		t.Fatalf("UIFillerMinDelay = %s, want 1600ms", cfg.UIFillerMinDelay)
	}
	if cfg.UIFillerCooldown != 27*time.Second {
		t.Fatalf("UIFillerCooldown = %s, want 27s", cfg.UIFillerCooldown)
	}
	if cfg.UIFillerMaxPerTurn != 2 {
		t.Fatalf("UIFillerMaxPerTurn = %d, want 2", cfg.UIFillerMaxPerTurn)
	}
	if cfg.LocalSTTProfile != "fast" {
		t.Fatalf("LocalSTTProfile = %q, want %q", cfg.LocalSTTProfile, "fast")
	}
	if cfg.LocalSTTAutoDownload {
		t.Fatalf("LocalSTTAutoDownload = true, want false")
	}
	if !cfg.UITaskDeskDefault {
		t.Fatalf("UITaskDeskDefault = false, want true")
	}
	if cfg.UIVADProfile != "default" {
		t.Fatalf("UIVADProfile = %q, want %q", cfg.UIVADProfile, "default")
	}
	if !cfg.OpenClawHTTPStreamStrict {
		t.Fatalf("OpenClawHTTPStreamStrict = false, want true")
	}
}

func TestLoadAcceptsPatientVADProfile(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9595")
	t.Setenv("APP_UI_VAD_PROFILE", "patient")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UIVADProfile != "patient" {
		t.Fatalf("UIVADProfile = %q, want %q", cfg.UIVADProfile, "patient")
	}
}

func TestLoadRejectsInvalidAudioSegmentOverlap(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9696")
	t.Setenv("APP_UI_AUDIO_SEGMENT_OVERLAP", "500ms")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid APP_UI_AUDIO_SEGMENT_OVERLAP")
	}
	if !strings.Contains(err.Error(), "APP_UI_AUDIO_SEGMENT_OVERLAP") {
		t.Fatalf("Load() error = %v, want APP_UI_AUDIO_SEGMENT_OVERLAP validation error", err)
	}
}

func TestLoadRejectsInvalidFillerMode(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9797")
	t.Setenv("APP_FILLER_MODE", "sometimes")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid APP_FILLER_MODE")
	}
	if !strings.Contains(err.Error(), "APP_FILLER_MODE") {
		t.Fatalf("Load() error = %v, want APP_FILLER_MODE validation error", err)
	}
}

func TestLoadRejectsInvalidLocalSTTProfile(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9898")
	t.Setenv("APP_LOCAL_STT_PROFILE", "ultra")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() expected error for invalid APP_LOCAL_STT_PROFILE")
	}
	if !strings.Contains(err.Error(), "APP_LOCAL_STT_PROFILE") {
		t.Fatalf("Load() error = %v, want APP_LOCAL_STT_PROFILE validation error", err)
	}
}

func TestLoadAppliesLocalSTTProfilePresetDefaults(t *testing.T) {
	setCoreEnvEmpty(t)
	t.Setenv("APP_BIND_ADDR", ":9990")
	t.Setenv("APP_LOCAL_STT_PROFILE", "accurate")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LocalWhisperModelPath != ".models/whisper/ggml-small.en.bin" {
		t.Fatalf("LocalWhisperModelPath = %q, want %q", cfg.LocalWhisperModelPath, ".models/whisper/ggml-small.en.bin")
	}
	if cfg.LocalWhisperBeamSize != 3 {
		t.Fatalf("LocalWhisperBeamSize = %d, want 3", cfg.LocalWhisperBeamSize)
	}
	if cfg.LocalWhisperBestOf != 3 {
		t.Fatalf("LocalWhisperBestOf = %d, want 3", cfg.LocalWhisperBestOf)
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
		"APP_ASSISTANT_WORKING_DELAY",
		"APP_METRICS_NAMESPACE",
		"APP_ALLOW_ANY_ORIGIN",
		"APP_STRICT_OUTBOUND",
		"APP_UI_AUDIO_WORKLET",
		"APP_UI_SILENCE_BREAKER_MODE",
		"APP_UI_SILENCE_BREAKER_DELAY",
		"APP_UI_VAD_MIN_UTTERANCE",
		"APP_UI_VAD_GRACE",
		"APP_UI_AUDIO_SEGMENT_OVERLAP",
		"APP_FILLER_MODE",
		"APP_FILLER_MIN_DELAY",
		"APP_FILLER_COOLDOWN",
		"APP_FILLER_MAX_PER_TURN",
		"APP_UI_TASK_DESK_DEFAULT",
		"APP_UI_VAD_PROFILE",
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
		"APP_LOCAL_STT_PROFILE",
		"APP_LOCAL_STT_AUTO_MODEL_DOWNLOAD",
		"LOCAL_KOKORO_PYTHON",
		"LOCAL_KOKORO_WORKER_SCRIPT",
		"LOCAL_KOKORO_VOICE",
		"LOCAL_KOKORO_LANG_CODE",
		"OPENCLAW_ADAPTER_MODE",
		"OPENCLAW_HTTP_URL",
		"OPENCLAW_CLI_PATH",
		"OPENCLAW_CLI_THINKING",
		"OPENCLAW_CLI_STREAMING",
		"OPENCLAW_CLI_STREAM_MIN_CHARS",
		"OPENCLAW_HTTP_STREAM_STRICT",
		"DATABASE_URL",
		"MEMORY_EMBEDDING_DIM",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
