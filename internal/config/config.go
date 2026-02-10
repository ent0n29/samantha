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
	FirstAudioSLO            time.Duration
	MetricsNamespace         string

	AllowAnyOrigin bool

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

	LocalKokoroPython       string
	LocalKokoroWorkerScript string
	LocalKokoroVoice        string
	LocalKokoroLangCode     string

	OpenClawAdapterMode string
	OpenClawHTTPURL     string
	OpenClawCLIPath     string

	DatabaseURL        string
	MemoryEmbeddingDim int
}

// Load reads environment variables and applies safe defaults.
func Load() (Config, error) {
	cfg := Config{
		BindAddr:            envOrDefault("APP_BIND_ADDR", ":8080"),
		MetricsNamespace:    envOrDefault("APP_METRICS_NAMESPACE", "samantha"),
		AllowAnyOrigin:      false,
		VoiceProvider:       envOrDefault("VOICE_PROVIDER", "auto"),
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
		// Default to a fast multilingual Whisper model for local realtime use.
		LocalWhisperModelPath: envOrDefault("LOCAL_WHISPER_MODEL_PATH", ".models/whisper/ggml-base.bin"),
		LocalWhisperLanguage:  envOrDefault("LOCAL_WHISPER_LANGUAGE", "en"),
		// 0 means "auto" (picked based on CPU count).
		LocalWhisperThreads:      0,
		LocalWhisperBeamSize:     1,
		LocalWhisperBestOf:       1,
		LocalKokoroPython:        envOrDefault("LOCAL_KOKORO_PYTHON", ""),
		LocalKokoroWorkerScript:  envOrDefault("LOCAL_KOKORO_WORKER_SCRIPT", "scripts/kokoro_worker.py"),
		LocalKokoroVoice:         envOrDefault("LOCAL_KOKORO_VOICE", "af_heart"),
		LocalKokoroLangCode:      envOrDefault("LOCAL_KOKORO_LANG_CODE", "a"),
		OpenClawAdapterMode:      envOrDefault("OPENCLAW_ADAPTER_MODE", "auto"),
		OpenClawHTTPURL:          stringsTrimSpace("OPENCLAW_HTTP_URL"),
		OpenClawCLIPath:          envOrDefault("OPENCLAW_CLI_PATH", "openclaw"),
		ElevenLabsAPIKey:         stringsTrimSpace("ELEVENLABS_API_KEY"),
		DatabaseURL:              stringsTrimSpace("DATABASE_URL"),
		MemoryEmbeddingDim:       1536,
		ShutdownTimeout:          15 * time.Second,
		SessionInactivityTimeout: 2 * time.Minute,
		FirstAudioSLO:            700 * time.Millisecond,
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
	cfg.FirstAudioSLO, err = durationFromEnv("APP_FIRST_AUDIO_SLO", cfg.FirstAudioSLO)
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

	if cfg.SessionInactivityTimeout < 5*time.Second {
		return Config{}, fmt.Errorf("APP_SESSION_INACTIVITY_TIMEOUT must be at least 5s")
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
