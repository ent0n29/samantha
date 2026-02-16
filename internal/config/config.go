package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains all runtime settings for the companion voice service.
type Config struct {
	BindAddr                 string
	ShutdownTimeout          time.Duration
	SessionInactivityTimeout time.Duration
	SessionRetention         time.Duration
	FirstAudioSLO            time.Duration
	AssistantWorkingDelay    time.Duration
	UISilenceBreakerDelay    time.Duration
	UIVADMinUtterance        time.Duration
	UIVADGrace               time.Duration
	UIAudioSegmentOverlap    time.Duration
	UIFillerMinDelay         time.Duration
	UIFillerCooldown         time.Duration
	MetricsNamespace         string
	TaskRuntimeEnabled       bool
	TaskTimeout              time.Duration
	TaskIdempotencyWindow    time.Duration

	AllowAnyOrigin    bool
	StrictOutbound    bool
	UIAudioWorklet    bool
	UITaskDeskDefault bool

	UISilenceBreakerMode string
	UIVADProfile         string
	UIFillerMode         string

	WSBackpressureMode string

	VoiceProvider string

	ElevenLabsAPIKey            string
	ElevenLabsWSBaseURL         string
	ElevenLabsTTSVoice          string
	ElevenLabsTTSModel          string
	ElevenLabsSTTModel          string
	ElevenLabsTTSOutputFormat   string
	ElevenLabsSTTCommitStrategy string

	LocalWhisperCLI       string
	LocalWhisperModelPath string
	LocalWhisperLanguage  string
	LocalWhisperThreads   int
	LocalWhisperBeamSize  int
	LocalWhisperBestOf    int
	LocalSTTProfile       string
	LocalSTTAutoDownload  bool

	LocalKokoroPython       string
	LocalKokoroWorkerScript string
	LocalKokoroVoice        string
	LocalKokoroLangCode     string
	UIFillerMaxPerTurn      int

	OpenClawAdapterMode       string
	OpenClawGatewayURL        string
	OpenClawGatewayToken      string
	OpenClawHTTPURL           string
	OpenClawCLIPath           string
	OpenClawCLIThinking       string
	OpenClawCLIStreaming      bool
	OpenClawCLIStreamMinChars int
	OpenClawHTTPStreamStrict  bool

	DatabaseURL        string
	MemoryEmbeddingDim int
}

// Load reads environment variables and applies safe defaults.
func Load() (Config, error) {
	localSTTProfile := envOrDefault("APP_LOCAL_STT_PROFILE", "balanced")
	presetModelPath, presetBeamSize, presetBestOf := localSTTPresetDefaults(localSTTProfile)

	cfg := Config{
		BindAddr:            envOrDefault("APP_BIND_ADDR", ":8080"),
		MetricsNamespace:    envOrDefault("APP_METRICS_NAMESPACE", "samantha"),
		AllowAnyOrigin:      false,
		VoiceProvider:       envOrDefault("VOICE_PROVIDER", "local"),
		ElevenLabsWSBaseURL: envOrDefault("ELEVENLABS_WS_BASE_URL", "wss://api.elevenlabs.io"),
		// Default to a warm female premade voice for the Samantha prototype.
		ElevenLabsTTSVoice: envOrDefault("ELEVENLABS_TTS_VOICE_ID", "cgSgspJ2msm6clMCkdW9"),
		ElevenLabsTTSModel: envOrDefault("ELEVENLABS_TTS_MODEL_ID", "eleven_multilingual_v2"),
		ElevenLabsSTTModel: envOrDefault("ELEVENLABS_STT_MODEL_ID", "scribe_v2_realtime"),
		// Prefer low-latency PCM for realtime playback; preview endpoint wraps it as WAV.
		ElevenLabsTTSOutputFormat: envOrDefault("ELEVENLABS_TTS_OUTPUT_FORMAT", "pcm_16000"),
		// Prefer explicit commit driven by our client-side VAD and controls.
		ElevenLabsSTTCommitStrategy: envOrDefault("ELEVENLABS_STT_COMMIT_STRATEGY", "manual"),
		LocalWhisperCLI:             envOrDefault("LOCAL_WHISPER_CLI", "whisper-cli"),
		LocalWhisperModelPath:       envOrDefault("LOCAL_WHISPER_MODEL_PATH", presetModelPath),
		LocalWhisperLanguage:        envOrDefault("LOCAL_WHISPER_LANGUAGE", "en"),
		// 0 means "auto" (picked based on CPU count).
		LocalWhisperThreads:       0,
		LocalWhisperBeamSize:      presetBeamSize,
		LocalWhisperBestOf:        presetBestOf,
		LocalSTTProfile:           localSTTProfile,
		LocalSTTAutoDownload:      true,
		LocalKokoroPython:         envOrDefault("LOCAL_KOKORO_PYTHON", ""),
		LocalKokoroWorkerScript:   envOrDefault("LOCAL_KOKORO_WORKER_SCRIPT", "scripts/kokoro_worker.py"),
		LocalKokoroVoice:          envOrDefault("LOCAL_KOKORO_VOICE", "af_sarah"),
		LocalKokoroLangCode:       envOrDefault("LOCAL_KOKORO_LANG_CODE", "a"),
		OpenClawAdapterMode:       envOrDefault("OPENCLAW_ADAPTER_MODE", "auto"),
		OpenClawGatewayURL:        envOrDefault("OPENCLAW_GATEWAY_URL", "ws://127.0.0.1:18789"),
		OpenClawGatewayToken:      stringsTrimSpace("OPENCLAW_GATEWAY_TOKEN"),
		OpenClawHTTPURL:           stringsTrimSpace("OPENCLAW_HTTP_URL"),
		OpenClawCLIPath:           envOrDefault("OPENCLAW_CLI_PATH", "openclaw"),
		OpenClawCLIThinking:       envOrDefault("OPENCLAW_CLI_THINKING", "minimal"),
		OpenClawCLIStreaming:      true,
		OpenClawCLIStreamMinChars: 16,
		ElevenLabsAPIKey:          stringsTrimSpace("ELEVENLABS_API_KEY"),
		DatabaseURL:               stringsTrimSpace("DATABASE_URL"),
		MemoryEmbeddingDim:        1536,
		ShutdownTimeout:           15 * time.Second,
		SessionInactivityTimeout:  2 * time.Minute,
		SessionRetention:          24 * time.Hour,
		FirstAudioSLO:             700 * time.Millisecond,
		AssistantWorkingDelay:     500 * time.Millisecond,
		UISilenceBreakerDelay:     750 * time.Millisecond,
		UIVADMinUtterance:         650 * time.Millisecond,
		UIVADGrace:                220 * time.Millisecond,
		UIAudioSegmentOverlap:     22 * time.Millisecond,
		UIFillerMinDelay:          1200 * time.Millisecond,
		UIFillerCooldown:          18 * time.Second,
		UIFillerMaxPerTurn:        1,
		TaskRuntimeEnabled:        false,
		TaskTimeout:               20 * time.Minute,
		TaskIdempotencyWindow:     10 * time.Second,
		StrictOutbound:            false,
		UIAudioWorklet:            true,
		UITaskDeskDefault:         false,
		UISilenceBreakerMode:      envOrDefault("APP_UI_SILENCE_BREAKER_MODE", "visual"),
		UIVADProfile:              envOrDefault("APP_UI_VAD_PROFILE", "default"),
		UIFillerMode:              envOrDefault("APP_FILLER_MODE", "off"),
		WSBackpressureMode:        envOrDefault("APP_WS_BACKPRESSURE_MODE", "drop"),
		OpenClawHTTPStreamStrict:  false,
	}

	// Convenience: if the gateway token isn't set in the environment, try the per-repo
	// token file used by `scripts/dev` (gitignored). This makes the fast WS streaming
	// path work even when starting the server without `make dev`.
	if strings.TrimSpace(cfg.OpenClawGatewayToken) == "" {
		if raw, err := os.ReadFile(".tmp/openclaw_gateway_token"); err == nil {
			if tok := strings.TrimSpace(string(raw)); tok != "" {
				cfg.OpenClawGatewayToken = tok
			}
		}
	}

	var err error
	cfg.ShutdownTimeout, err = durationFromEnv("APP_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.SessionInactivityTimeout, err = durationFromEnv("APP_SESSION_INACTIVITY_TIMEOUT", cfg.SessionInactivityTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.SessionRetention, err = durationFromEnv("APP_SESSION_RETENTION", cfg.SessionRetention)
	if err != nil {
		return Config{}, err
	}
	cfg.FirstAudioSLO, err = durationFromEnv("APP_FIRST_AUDIO_SLO", cfg.FirstAudioSLO)
	if err != nil {
		return Config{}, err
	}
	cfg.AssistantWorkingDelay, err = durationFromEnv("APP_ASSISTANT_WORKING_DELAY", cfg.AssistantWorkingDelay)
	if err != nil {
		return Config{}, err
	}
	cfg.UISilenceBreakerDelay, err = durationFromEnv("APP_UI_SILENCE_BREAKER_DELAY", cfg.UISilenceBreakerDelay)
	if err != nil {
		return Config{}, err
	}
	cfg.UIVADMinUtterance, err = durationFromEnv("APP_UI_VAD_MIN_UTTERANCE", cfg.UIVADMinUtterance)
	if err != nil {
		return Config{}, err
	}
	cfg.UIVADGrace, err = durationFromEnv("APP_UI_VAD_GRACE", cfg.UIVADGrace)
	if err != nil {
		return Config{}, err
	}
	cfg.UIAudioSegmentOverlap, err = durationFromEnv("APP_UI_AUDIO_SEGMENT_OVERLAP", cfg.UIAudioSegmentOverlap)
	if err != nil {
		return Config{}, err
	}
	cfg.UIFillerMinDelay, err = durationFromEnv("APP_FILLER_MIN_DELAY", cfg.UIFillerMinDelay)
	if err != nil {
		return Config{}, err
	}
	cfg.UIFillerCooldown, err = durationFromEnv("APP_FILLER_COOLDOWN", cfg.UIFillerCooldown)
	if err != nil {
		return Config{}, err
	}
	cfg.TaskTimeout, err = durationFromEnv("APP_TASK_TIMEOUT", cfg.TaskTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.TaskIdempotencyWindow, err = durationFromEnv("APP_TASK_IDEMPOTENCY_WINDOW", cfg.TaskIdempotencyWindow)
	if err != nil {
		return Config{}, err
	}
	cfg.MemoryEmbeddingDim, err = intFromEnv("MEMORY_EMBEDDING_DIM", cfg.MemoryEmbeddingDim)
	if err != nil {
		return Config{}, err
	}
	cfg.AllowAnyOrigin, err = boolFromEnv("APP_ALLOW_ANY_ORIGIN", cfg.AllowAnyOrigin)
	if err != nil {
		return Config{}, err
	}
	cfg.StrictOutbound, err = boolFromEnv("APP_STRICT_OUTBOUND", cfg.StrictOutbound)
	if err != nil {
		return Config{}, err
	}
	cfg.UIAudioWorklet, err = boolFromEnv("APP_UI_AUDIO_WORKLET", cfg.UIAudioWorklet)
	if err != nil {
		return Config{}, err
	}
	cfg.UITaskDeskDefault, err = boolFromEnv("APP_UI_TASK_DESK_DEFAULT", cfg.UITaskDeskDefault)
	if err != nil {
		return Config{}, err
	}
	cfg.TaskRuntimeEnabled, err = boolFromEnv("APP_TASK_RUNTIME_ENABLED", cfg.TaskRuntimeEnabled)
	if err != nil {
		return Config{}, err
	}
	cfg.LocalSTTAutoDownload, err = boolFromEnv("APP_LOCAL_STT_AUTO_MODEL_DOWNLOAD", cfg.LocalSTTAutoDownload)
	if err != nil {
		return Config{}, err
	}
	cfg.OpenClawHTTPStreamStrict, err = boolFromEnv("OPENCLAW_HTTP_STREAM_STRICT", cfg.OpenClawHTTPStreamStrict)
	if err != nil {
		return Config{}, err
	}
	cfg.OpenClawCLIStreaming, err = boolFromEnv("OPENCLAW_CLI_STREAMING", cfg.OpenClawCLIStreaming)
	if err != nil {
		return Config{}, err
	}

	cfg.LocalWhisperThreads, err = intFromEnv("LOCAL_WHISPER_THREADS", cfg.LocalWhisperThreads)
	if err != nil {
		return Config{}, err
	}
	cfg.LocalWhisperBeamSize, err = intFromEnv("LOCAL_WHISPER_BEAM_SIZE", cfg.LocalWhisperBeamSize)
	if err != nil {
		return Config{}, err
	}
	cfg.LocalWhisperBestOf, err = intFromEnv("LOCAL_WHISPER_BEST_OF", cfg.LocalWhisperBestOf)
	if err != nil {
		return Config{}, err
	}
	cfg.OpenClawCLIStreamMinChars, err = intFromEnv("OPENCLAW_CLI_STREAM_MIN_CHARS", cfg.OpenClawCLIStreamMinChars)
	if err != nil {
		return Config{}, err
	}
	cfg.UIFillerMaxPerTurn, err = intFromEnv("APP_FILLER_MAX_PER_TURN", cfg.UIFillerMaxPerTurn)
	if err != nil {
		return Config{}, err
	}

	if cfg.SessionInactivityTimeout < 5*time.Second {
		return Config{}, fmt.Errorf("APP_SESSION_INACTIVITY_TIMEOUT must be at least 5s")
	}
	if cfg.SessionRetention < 0 {
		return Config{}, fmt.Errorf("APP_SESSION_RETENTION must be >= 0")
	}
	if cfg.AssistantWorkingDelay < 0 {
		return Config{}, fmt.Errorf("APP_ASSISTANT_WORKING_DELAY must be >= 0")
	}
	if cfg.UISilenceBreakerDelay < 0 {
		return Config{}, fmt.Errorf("APP_UI_SILENCE_BREAKER_DELAY must be >= 0")
	}
	if cfg.UIVADMinUtterance < 0 {
		return Config{}, fmt.Errorf("APP_UI_VAD_MIN_UTTERANCE must be >= 0")
	}
	if cfg.UIVADGrace < 0 {
		return Config{}, fmt.Errorf("APP_UI_VAD_GRACE must be >= 0")
	}
	if cfg.UIAudioSegmentOverlap < 0 {
		return Config{}, fmt.Errorf("APP_UI_AUDIO_SEGMENT_OVERLAP must be >= 0")
	}
	if cfg.UIAudioSegmentOverlap > 150*time.Millisecond {
		return Config{}, fmt.Errorf("APP_UI_AUDIO_SEGMENT_OVERLAP must be <= 150ms")
	}
	if cfg.UIFillerMinDelay < 0 {
		return Config{}, fmt.Errorf("APP_FILLER_MIN_DELAY must be >= 0")
	}
	if cfg.UIFillerCooldown < 0 {
		return Config{}, fmt.Errorf("APP_FILLER_COOLDOWN must be >= 0")
	}
	if cfg.UIFillerMaxPerTurn < 0 {
		return Config{}, fmt.Errorf("APP_FILLER_MAX_PER_TURN must be >= 0")
	}
	if cfg.UIFillerMaxPerTurn > 10 {
		return Config{}, fmt.Errorf("APP_FILLER_MAX_PER_TURN must be <= 10")
	}
	if cfg.TaskTimeout <= 0 {
		return Config{}, fmt.Errorf("APP_TASK_TIMEOUT must be > 0")
	}
	if cfg.TaskIdempotencyWindow <= 0 {
		return Config{}, fmt.Errorf("APP_TASK_IDEMPOTENCY_WINDOW must be > 0")
	}
	cfg.WSBackpressureMode = strings.ToLower(trimSpace(cfg.WSBackpressureMode))
	if cfg.WSBackpressureMode == "" {
		cfg.WSBackpressureMode = "drop"
	}
	if cfg.WSBackpressureMode != "drop" && cfg.WSBackpressureMode != "block" {
		return Config{}, fmt.Errorf("APP_WS_BACKPRESSURE_MODE must be one of: drop|block")
	}
	cfg.UISilenceBreakerMode = strings.ToLower(trimSpace(cfg.UISilenceBreakerMode))
	if cfg.UISilenceBreakerMode == "" {
		cfg.UISilenceBreakerMode = "visual"
	}
	if cfg.UISilenceBreakerMode != "off" && cfg.UISilenceBreakerMode != "visual" && cfg.UISilenceBreakerMode != "speech" {
		return Config{}, fmt.Errorf("APP_UI_SILENCE_BREAKER_MODE must be one of: off|visual|speech")
	}
	cfg.UIVADProfile = strings.ToLower(trimSpace(cfg.UIVADProfile))
	if cfg.UIVADProfile == "" {
		cfg.UIVADProfile = "default"
	}
	if cfg.UIVADProfile != "snappy" && cfg.UIVADProfile != "default" && cfg.UIVADProfile != "patient" {
		return Config{}, fmt.Errorf("APP_UI_VAD_PROFILE must be one of: default|patient|snappy")
	}
	cfg.UIFillerMode = strings.ToLower(trimSpace(cfg.UIFillerMode))
	if cfg.UIFillerMode == "" {
		cfg.UIFillerMode = "off"
	}
	if cfg.UIFillerMode != "off" && cfg.UIFillerMode != "adaptive" && cfg.UIFillerMode != "occasional" && cfg.UIFillerMode != "always" {
		return Config{}, fmt.Errorf("APP_FILLER_MODE must be one of: off|adaptive|occasional|always")
	}
	cfg.LocalSTTProfile = strings.ToLower(trimSpace(cfg.LocalSTTProfile))
	if cfg.LocalSTTProfile == "" {
		cfg.LocalSTTProfile = "balanced"
	}
	if cfg.LocalSTTProfile != "fast" && cfg.LocalSTTProfile != "balanced" && cfg.LocalSTTProfile != "accurate" {
		return Config{}, fmt.Errorf("APP_LOCAL_STT_PROFILE must be one of: fast|balanced|accurate")
	}
	if cfg.MemoryEmbeddingDim <= 0 {
		return Config{}, fmt.Errorf("MEMORY_EMBEDDING_DIM must be positive")
	}
	if cfg.LocalWhisperThreads < 0 {
		return Config{}, fmt.Errorf("LOCAL_WHISPER_THREADS must be >= 0")
	}
	if cfg.LocalWhisperBeamSize <= 0 {
		return Config{}, fmt.Errorf("LOCAL_WHISPER_BEAM_SIZE must be positive")
	}
	if cfg.LocalWhisperBestOf <= 0 {
		return Config{}, fmt.Errorf("LOCAL_WHISPER_BEST_OF must be positive")
	}
	cfg.OpenClawCLIThinking = strings.ToLower(trimSpace(cfg.OpenClawCLIThinking))
	if cfg.OpenClawCLIThinking == "" {
		cfg.OpenClawCLIThinking = "minimal"
	}
	switch cfg.OpenClawCLIThinking {
	case "minimal", "low", "medium", "high":
	default:
		return Config{}, fmt.Errorf("OPENCLAW_CLI_THINKING must be one of: minimal|low|medium|high")
	}
	if cfg.OpenClawCLIStreamMinChars <= 0 {
		return Config{}, fmt.Errorf("OPENCLAW_CLI_STREAM_MIN_CHARS must be > 0")
	}
	if cfg.OpenClawCLIStreamMinChars > 2048 {
		return Config{}, fmt.Errorf("OPENCLAW_CLI_STREAM_MIN_CHARS must be <= 2048")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func stringsTrimSpace(key string) string {
	return trimSpace(os.Getenv(key))
}

func trimSpace(v string) string {
	for len(v) > 0 && (v[0] == ' ' || v[0] == '\n' || v[0] == '\t' || v[0] == '\r') {
		v = v[1:]
	}
	for len(v) > 0 {
		c := v[len(v)-1]
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			v = v[:len(v)-1]
			continue
		}
		break
	}
	return v
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := stringsTrimSpace(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s parse error: %w", key, err)
	}
	return d, nil
}

func intFromEnv(key string, fallback int) (int, error) {
	v := stringsTrimSpace(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s parse error: %w", key, err)
	}
	return n, nil
}

func boolFromEnv(key string, fallback bool) (bool, error) {
	v := strings.ToLower(stringsTrimSpace(key))
	if v == "" {
		return fallback, nil
	}
	switch v {
	case "1", "true", "t", "yes", "y", "on":
		return true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s parse error: expected bool", key)
	}
}

func localSTTPresetDefaults(profile string) (modelPath string, beamSize int, bestOf int) {
	switch strings.ToLower(trimSpace(profile)) {
	case "fast":
		return ".models/whisper/ggml-tiny.en.bin", 1, 1
	case "accurate":
		return ".models/whisper/ggml-small.en.bin", 3, 3
	default:
		return ".models/whisper/ggml-base.en.bin", 2, 2
	}
}
