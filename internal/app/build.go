package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ent0n29/samantha/internal/config"
	"github.com/ent0n29/samantha/internal/httpapi"
	"github.com/ent0n29/samantha/internal/memory"
	"github.com/ent0n29/samantha/internal/observability"
	"github.com/ent0n29/samantha/internal/openclaw"
	"github.com/ent0n29/samantha/internal/session"
	"github.com/ent0n29/samantha/internal/taskruntime"
	"github.com/ent0n29/samantha/internal/voice"
)

type VoiceInfo struct {
	Provider       string
	Detail         string
	DefaultVoiceID string
	DefaultModelID string
}

type BuildResult struct {
	Config       config.Config
	API          *httpapi.Server
	Sessions     *session.Manager
	Orchestrator *voice.Orchestrator
	TaskService  *taskruntime.Service
	Metrics      *observability.Metrics
	Voice        VoiceInfo

	// Cleanup should be called on shutdown to release external resources (DB, local workers, etc).
	Cleanup func() error
}

func Build(ctx context.Context, cfg config.Config) (*BuildResult, error) {
	metrics := observability.NewMetrics(cfg.MetricsNamespace)

	memoryStore, err := memory.NewStore(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("memory store init failed: %w", err)
	}

	adapter, err := openclaw.NewAdapter(openclaw.Config{
		Mode:              cfg.OpenClawAdapterMode,
		HTTPURL:           cfg.OpenClawHTTPURL,
		CLIPath:           cfg.OpenClawCLIPath,
		CLIThinking:       cfg.OpenClawCLIThinking,
		CLIStreaming:      cfg.OpenClawCLIStreaming,
		CLIStreamMinChars: cfg.OpenClawCLIStreamMinChars,
		HTTPStreamStrict:  cfg.OpenClawHTTPStreamStrict,
	})
	if err != nil {
		_ = memoryStore.Close()
		return nil, fmt.Errorf("openclaw adapter init failed: %w", err)
	}

	voiceSetup, err := resolveVoiceProviders(cfg)
	if err != nil {
		_ = memoryStore.Close()
		return nil, err
	}

	taskService := taskruntime.New(taskruntime.Config{
		Enabled:           cfg.TaskRuntimeEnabled,
		TaskTimeout:       cfg.TaskTimeout,
		IdempotencyWindow: cfg.TaskIdempotencyWindow,
		DatabaseURL:       cfg.DatabaseURL,
	}, adapter, metrics)

	// Ensure API handlers know which backend is active (e.g. voices list).
	cfg.VoiceProvider = voiceSetup.resolvedProvider

	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	sessions.SetEndedRetention(cfg.SessionRetention)
	sessions.SetExpireHook(func(_ *session.Session) {
		metrics.SessionEvents.WithLabelValues("expired").Inc()
		metrics.ActiveSessions.Set(float64(sessions.ActiveCount()))
	})

	orchestrator := voice.NewOrchestrator(
		sessions,
		adapter,
		memoryStore,
		voiceSetup.sttProvider,
		voiceSetup.ttsProvider,
		metrics,
		cfg.FirstAudioSLO,
		cfg.AssistantWorkingDelay,
		voiceSetup.defaultVoiceID,
		voiceSetup.defaultModelID,
		cfg.VoiceProvider,
		cfg.StrictOutbound,
		cfg.WSBackpressureMode,
		taskService,
	)

	api := httpapi.New(cfg, sessions, orchestrator, metrics, taskService)

	cleanup := func() error {
		var errs []string
		if taskService != nil {
			if err := taskService.Close(); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if voiceSetup.cleanup != nil {
			if err := voiceSetup.cleanup(); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if err := memoryStore.Close(); err != nil {
			errs = append(errs, err.Error())
		}
		if len(errs) > 0 {
			return fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return nil
	}

	return &BuildResult{
		Config:       cfg,
		API:          api,
		Sessions:     sessions,
		Orchestrator: orchestrator,
		TaskService:  taskService,
		Metrics:      metrics,
		Voice: VoiceInfo{
			Provider:       cfg.VoiceProvider,
			Detail:         voiceSetup.detail,
			DefaultVoiceID: voiceSetup.defaultVoiceID,
			DefaultModelID: voiceSetup.defaultModelID,
		},
		Cleanup: cleanup,
	}, nil
}
