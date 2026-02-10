package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/antoniostano/samantha/internal/config"
	"github.com/antoniostano/samantha/internal/httpapi"
	"github.com/antoniostano/samantha/internal/memory"
	"github.com/antoniostano/samantha/internal/observability"
	"github.com/antoniostano/samantha/internal/openclaw"
	"github.com/antoniostano/samantha/internal/session"
	"github.com/antoniostano/samantha/internal/voice"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	metrics := observability.NewMetrics(cfg.MetricsNamespace)

	ctx := context.Background()
	memoryStore, err := memory.NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("memory store init failed: %v", err)
	}
	defer memoryStore.Close()

	adapter, err := openclaw.NewAdapter(openclaw.Config{
		Mode:    cfg.OpenClawAdapterMode,
		HTTPURL: cfg.OpenClawHTTPURL,
		CLIPath: cfg.OpenClawCLIPath,
	})
	if err != nil {
		log.Fatalf("openclaw adapter init failed: %v", err)
	}

	var (
		sttProvider           voice.STTProvider
		ttsProvider           voice.TTSProvider
		resolvedVoiceProvider string
		defaultVoiceID        string
		defaultModelID        string
	)

	voiceMode := strings.ToLower(strings.TrimSpace(cfg.VoiceProvider))
	if voiceMode == "" {
		voiceMode = "auto"
	}

	tryElevenLabs := func() bool {
		if strings.TrimSpace(cfg.ElevenLabsAPIKey) == "" {
			return false
		}
		p := voice.NewElevenLabsProvider(voice.ElevenLabsConfig{
			APIKey:              cfg.ElevenLabsAPIKey,
			WSBaseURL:           cfg.ElevenLabsWSBaseURL,
			STTModelID:          cfg.ElevenLabsSTTModel,
			DefaultOutputFormat: "mp3_44100_128",
		})
		sttProvider = p
		ttsProvider = p
		resolvedVoiceProvider = "elevenlabs"
		defaultVoiceID = cfg.ElevenLabsTTSVoice
		defaultModelID = cfg.ElevenLabsTTSModel
		log.Printf("voice provider: elevenlabs realtime")
		return true
	}

	tryLocal := func(fatal bool) bool {
		p, err := voice.NewLocalProvider(voice.LocalConfig{
			WhisperCLI:         cfg.LocalWhisperCLI,
			WhisperModelPath:   cfg.LocalWhisperModelPath,
			WhisperLanguage:    cfg.LocalWhisperLanguage,
			WhisperThreads:     cfg.LocalWhisperThreads,
			WhisperBeamSize:    cfg.LocalWhisperBeamSize,
			WhisperBestOf:      cfg.LocalWhisperBestOf,
			KokoroPython:       cfg.LocalKokoroPython,
			KokoroWorkerScript: cfg.LocalKokoroWorkerScript,
			KokoroVoice:        cfg.LocalKokoroVoice,
			KokoroLangCode:     cfg.LocalKokoroLangCode,
		})
		if err != nil {
			if fatal {
				log.Fatalf("local voice provider init failed: %v", err)
			}
			log.Printf("local voice provider unavailable: %v", err)
			return false
		}
		sttProvider = p
		ttsProvider = p
		resolvedVoiceProvider = "local"
		defaultVoiceID = cfg.LocalKokoroVoice
		if strings.TrimSpace(defaultVoiceID) == "" {
			defaultVoiceID = "af_heart"
		}
		defaultModelID = "kokoro"
		log.Printf("voice provider: local (%s + kokoro)", p.STTBackend())
		return true
	}

	switch voiceMode {
	case "elevenlabs":
		if !tryElevenLabs() {
			log.Fatalf("VOICE_PROVIDER=elevenlabs but ELEVENLABS_API_KEY is not set")
		}
	case "local":
		_ = tryLocal(true)
	case "mock":
		p := voice.NewMockProvider()
		sttProvider = p
		ttsProvider = p
		resolvedVoiceProvider = "mock"
		defaultVoiceID = ""
		defaultModelID = ""
		log.Printf("voice provider: mock")
	case "auto":
		if tryElevenLabs() {
			break
		}
		if tryLocal(false) {
			break
		}
		p := voice.NewMockProvider()
		sttProvider = p
		ttsProvider = p
		resolvedVoiceProvider = "mock"
		log.Printf("voice provider: mock (no elevenlabs key and local voice unavailable)")
	default:
		log.Fatalf("invalid VOICE_PROVIDER: %q (expected auto|elevenlabs|local|mock)", cfg.VoiceProvider)
	}

	// Best-effort cleanup for local worker processes (kokoro, whisper-server, etc).
	if c, ok := sttProvider.(interface{ Close() error }); ok {
		defer c.Close()
	}

	// Ensure API handlers know which backend is active (e.g. voices list).
	cfg.VoiceProvider = resolvedVoiceProvider

	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	sessions.SetExpireHook(func(_ *session.Session) {
		metrics.SessionEvents.WithLabelValues("expired").Inc()
		metrics.ActiveSessions.Set(float64(sessions.ActiveCount()))
	})

	orchestrator := voice.NewOrchestrator(
		sessions,
		adapter,
		memoryStore,
		sttProvider,
		ttsProvider,
		metrics,
		cfg.FirstAudioSLO,
		defaultVoiceID,
		defaultModelID,
		cfg.VoiceProvider,
	)

	api := httpapi.New(cfg, sessions, orchestrator, metrics)
	httpServer := &http.Server{
		Addr:    cfg.BindAddr,
		Handler: api.Router(),
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	sessions.StartJanitor(runCtx, 5*time.Second)

	go func() {
		log.Printf("server listening on %s", cfg.BindAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("shutdown signal received")

	runCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		_ = httpServer.Close()
	}

	log.Printf("shutdown complete")
}
