package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type onboardingCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok|warn|error
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type onboardingStatusResponse struct {
	VoiceProvider      string            `json:"voice_provider"`
	BrainProvider      string            `json:"brain_provider"`
	TaskRuntimeEnabled bool              `json:"task_runtime_enabled"`
	TaskStoreMode      string            `json:"task_store_mode"`
	UIAudioWorklet     bool              `json:"ui_audio_worklet"`
	Checks             []onboardingCheck `json:"checks"`
}

func (s *Server) handleOnboardingStatus(w http.ResponseWriter, _ *http.Request) {
	voiceProvider := strings.ToLower(strings.TrimSpace(s.cfg.VoiceProvider))
	if voiceProvider == "" {
		voiceProvider = "auto"
	}

	brainProvider, brainChecks := s.brainChecks()
	taskRuntimeEnabled := s.taskService != nil && s.taskService.Enabled()
	if s.taskService == nil {
		taskRuntimeEnabled = s.cfg.TaskRuntimeEnabled
	}
	taskStoreMode := s.taskStoreMode()
	checks := make([]onboardingCheck, 0, 12)
	checks = append(checks, onboardingCheck{
		ID:     "voice_provider",
		Status: "ok",
		Label:  "Voice backend",
		Detail: voiceProvider,
	})
	if taskRuntimeEnabled {
		checks = append(checks, onboardingCheck{
			ID:     "task_runtime",
			Status: "ok",
			Label:  "Task runtime",
			Detail: fmt.Sprintf("enabled (%s store)", taskStoreMode),
		})
		switch taskStoreMode {
		case "postgres":
			checks = append(checks, onboardingCheck{
				ID:     "task_store",
				Status: "ok",
				Label:  "Task persistence",
				Detail: "postgres",
			})
		case "in-memory":
			checks = append(checks, onboardingCheck{
				ID:     "task_store",
				Status: "warn",
				Label:  "Task persistence",
				Detail: "in-memory only",
				Fix:    "Set DATABASE_URL to persist tasks across restarts.",
			})
		default:
			checks = append(checks, onboardingCheck{
				ID:     "task_store",
				Status: "warn",
				Label:  "Task persistence",
				Detail: taskStoreMode,
			})
		}
	} else {
		checks = append(checks, onboardingCheck{
			ID:     "task_runtime",
			Status: "warn",
			Label:  "Task runtime",
			Detail: "disabled",
			Fix:    "Set APP_TASK_RUNTIME_ENABLED=true to enable voice-to-task execution.",
		})
	}
	checks = append(checks, brainChecks...)

	switch voiceProvider {
	case "local":
		checks = append(checks, s.localVoiceChecks()...)
	case "elevenlabs":
		if strings.TrimSpace(s.cfg.ElevenLabsAPIKey) == "" {
			checks = append(checks, onboardingCheck{
				ID:     "elevenlabs_key",
				Status: "error",
				Label:  "ElevenLabs API key",
				Detail: "ELEVENLABS_API_KEY is not set",
				Fix:    "Set ELEVENLABS_API_KEY or switch to VOICE_PROVIDER=local.",
			})
		} else {
			checks = append(checks, onboardingCheck{
				ID:     "elevenlabs_key",
				Status: "ok",
				Label:  "ElevenLabs API key",
				Detail: "present",
			})
		}
	case "mock":
		checks = append(checks, onboardingCheck{
			ID:     "mock_voice",
			Status: "warn",
			Label:  "Voice backend is mock",
			Detail: "No STT/TTS audio will be generated.",
			Fix:    "Run `make setup-local-voice` and set VOICE_PROVIDER=local.",
		})
	default:
		checks = append(checks, onboardingCheck{
			ID:     "voice_provider_unknown",
			Status: "warn",
			Label:  "Voice backend",
			Detail: "unknown provider; expected local|elevenlabs|mock",
		})
	}

	respondJSON(w, http.StatusOK, onboardingStatusResponse{
		VoiceProvider:      voiceProvider,
		BrainProvider:      brainProvider,
		TaskRuntimeEnabled: taskRuntimeEnabled,
		TaskStoreMode:      taskStoreMode,
		UIAudioWorklet:     s.cfg.UIAudioWorklet,
		Checks:             checks,
	})
}

func (s *Server) localVoiceChecks() []onboardingCheck {
	fixAll := "Run `make setup-local-voice`, then restart `make dev`."
	out := make([]onboardingCheck, 0, 6)

	cli := strings.TrimSpace(s.cfg.LocalWhisperCLI)
	if cli == "" {
		cli = "whisper-cli"
	}
	if _, err := exec.LookPath(cli); err != nil {
		out = append(out, onboardingCheck{
			ID:     "whisper_cli",
			Status: "error",
			Label:  "Whisper (speech-to-text)",
			Detail: "whisper-cli not found",
			Fix:    fixAll,
		})
	} else {
		out = append(out, onboardingCheck{
			ID:     "whisper_cli",
			Status: "ok",
			Label:  "Whisper (speech-to-text)",
			Detail: "whisper-cli found",
		})
	}

	modelPath := strings.TrimSpace(s.cfg.LocalWhisperModelPath)
	if modelPath == "" {
		out = append(out, onboardingCheck{
			ID:     "whisper_model",
			Status: "error",
			Label:  "Whisper model",
			Detail: "LOCAL_WHISPER_MODEL_PATH is empty",
			Fix:    fixAll,
		})
	} else {
		if !filepath.IsAbs(modelPath) {
			if wd, err := os.Getwd(); err == nil {
				modelPath = filepath.Join(wd, modelPath)
			}
		}
		if _, err := os.Stat(modelPath); err != nil {
			out = append(out, onboardingCheck{
				ID:     "whisper_model",
				Status: "error",
				Label:  "Whisper model",
				Detail: "model file missing",
				Fix:    fixAll,
			})
		} else {
			out = append(out, onboardingCheck{
				ID:     "whisper_model",
				Status: "ok",
				Label:  "Whisper model",
				Detail: "present",
			})
		}
	}

	py := strings.TrimSpace(s.cfg.LocalKokoroPython)
	if py == "" {
		for _, candidate := range []string{".venv/bin/python3", ".venv/bin/python", "python3"} {
			if p, err := exec.LookPath(candidate); err == nil && strings.TrimSpace(p) != "" {
				py = p
				break
			}
		}
	} else {
		if p, err := exec.LookPath(py); err == nil && strings.TrimSpace(p) != "" {
			py = p
		}
	}

	if strings.TrimSpace(py) == "" {
		out = append(out, onboardingCheck{
			ID:     "kokoro_python",
			Status: "error",
			Label:  "Kokoro (text-to-speech)",
			Detail: "python not found",
			Fix:    fixAll,
		})
	} else {
		out = append(out, onboardingCheck{
			ID:     "kokoro_python",
			Status: "ok",
			Label:  "Kokoro (text-to-speech)",
			Detail: "python found",
		})
	}

	script := strings.TrimSpace(s.cfg.LocalKokoroWorkerScript)
	if script == "" {
		script = "scripts/kokoro_worker.py"
	}
	if !filepath.IsAbs(script) {
		if wd, err := os.Getwd(); err == nil {
			script = filepath.Join(wd, script)
		}
	}
	if _, err := os.Stat(script); err != nil {
		out = append(out, onboardingCheck{
			ID:     "kokoro_worker",
			Status: "error",
			Label:  "Kokoro worker script",
			Detail: "missing scripts/kokoro_worker.py",
			Fix:    "Verify repository files are present and up to date.",
		})
	} else {
		out = append(out, onboardingCheck{
			ID:     "kokoro_worker",
			Status: "ok",
			Label:  "Kokoro worker script",
			Detail: "present",
		})
	}

	return out
}

func (s *Server) brainChecks() (string, []onboardingCheck) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.OpenClawAdapterMode))
	if mode == "" {
		mode = "auto"
	}
	cli := strings.TrimSpace(s.cfg.OpenClawCLIPath)
	if cli == "" {
		cli = "openclaw"
	}
	httpURL := strings.TrimSpace(s.cfg.OpenClawHTTPURL)
	gatewayURL := strings.TrimSpace(s.cfg.OpenClawGatewayURL)
	gatewayToken := strings.TrimSpace(s.cfg.OpenClawGatewayToken)

	var resolved string
	checks := make([]onboardingCheck, 0, 6)

	stateDir := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR"))
	if stateDir == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			stateDir = filepath.Join(home, ".openclaw")
		}
	}
	identityPath := ""
	if stateDir != "" {
		identityPath = filepath.Join(stateDir, "identity", "device.json")
	}
	probeGatewayPort := func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			raw = "ws://127.0.0.1:18789"
		}
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		host := strings.TrimSpace(u.Host)
		if host == "" {
			return fmt.Errorf("host missing")
		}
		addr := host
		if !strings.Contains(host, ":") {
			// Fallback to a conservative default.
			addr = net.JoinHostPort(host, "80")
		}
		c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err != nil {
			return err
		}
		_ = c.Close()
		return nil
	}
	checkGateway := func(level string) bool {
		level = strings.ToLower(strings.TrimSpace(level))
		status := ""
		switch level {
		case "error", "fatal":
			status = "error"
		case "warn", "warning":
			status = "warn"
		case "silent", "":
			status = ""
		default:
			status = "warn"
		}

		if strings.TrimSpace(gatewayToken) == "" {
			if status != "" {
				checks = append(checks, onboardingCheck{
					ID:     "brain_gateway_token",
					Status: status,
					Label:  "Brain (OpenClaw Gateway)",
					Detail: "OPENCLAW_GATEWAY_TOKEN is missing",
					Fix:    "Run `make dev` (auto-generates .tmp/openclaw_gateway_token) or set OPENCLAW_GATEWAY_TOKEN.",
				})
			}
			return false
		}
		if identityPath == "" {
			if status != "" {
				checks = append(checks, onboardingCheck{
					ID:     "brain_gateway_identity",
					Status: status,
					Label:  "Brain (OpenClaw Gateway)",
					Detail: "cannot resolve OpenClaw identity path",
					Fix:    "Ensure HOME/OPENCLAW_STATE_DIR is set so OpenClaw state can be located.",
				})
			}
			return false
		}
		if _, err := os.Stat(identityPath); err != nil {
			if status != "" {
				checks = append(checks, onboardingCheck{
					ID:     "brain_gateway_identity",
					Status: status,
					Label:  "Brain (OpenClaw Gateway)",
					Detail: fmt.Sprintf("missing device identity (%s)", identityPath),
					Fix:    "Run `openclaw` once (or `make dev`) so OpenClaw creates identity/device.json.",
				})
			}
			return false
		}
		if err := probeGatewayPort(gatewayURL); err != nil {
			if status != "" {
				checks = append(checks, onboardingCheck{
					ID:     "brain_gateway_port",
					Status: status,
					Label:  "Brain (OpenClaw Gateway)",
					Detail: fmt.Sprintf("gateway not reachable (%s)", strings.TrimSpace(gatewayURL)),
					Fix:    "Start the gateway: `openclaw gateway --bind loopback --port 18789` (or run `make dev`).",
				})
			}
			return false
		}
		checks = append(checks, onboardingCheck{
			ID:     "brain_gateway",
			Status: "ok",
			Label:  "Brain (OpenClaw Gateway)",
			Detail: "streaming enabled",
		})
		return true
	}

	switch mode {
	case "gateway":
		resolved = "gateway"
		_ = checkGateway("error")
	case "cli":
		resolved = "cli"
		if _, err := exec.LookPath(cli); err != nil {
			checks = append(checks, onboardingCheck{
				ID:     "brain_cli",
				Status: "warn",
				Label:  "Brain (OpenClaw CLI)",
				Detail: "openclaw not found; falling back to mock",
				Fix:    "Install OpenClaw or set OPENCLAW_ADAPTER_MODE=mock.",
			})
			resolved = "mock"
		} else {
			checks = append(checks, onboardingCheck{
				ID:     "brain_cli",
				Status: "ok",
				Label:  "Brain (OpenClaw CLI)",
				Detail: "openclaw found",
			})
		}
	case "http":
		resolved = "http"
		if httpURL == "" {
			checks = append(checks, onboardingCheck{
				ID:     "brain_http",
				Status: "error",
				Label:  "Brain (OpenClaw HTTP)",
				Detail: "OPENCLAW_HTTP_URL is empty",
			})
		} else {
			checks = append(checks, onboardingCheck{
				ID:     "brain_http",
				Status: "ok",
				Label:  "Brain (OpenClaw HTTP)",
				Detail: "configured",
			})
		}
	case "mock":
		resolved = "mock"
		checks = append(checks, onboardingCheck{
			ID:     "brain_mock",
			Status: "warn",
			Label:  "Brain (mock)",
			Detail: "Responses are placeholders.",
			Fix:    "Install OpenClaw and use OPENCLAW_ADAPTER_MODE=auto.",
		})
	case "auto":
		if strings.TrimSpace(gatewayToken) != "" {
			if checkGateway("warn") {
				resolved = "gateway"
				return resolved, checks
			}
		}
		if cli != "" {
			if _, err := exec.LookPath(cli); err == nil {
				resolved = "cli"
				checks = append(checks, onboardingCheck{
					ID:     "brain_cli",
					Status: "ok",
					Label:  "Brain (OpenClaw CLI)",
					Detail: "openclaw found",
				})
				return resolved, checks
			}
		}
		if httpURL != "" {
			resolved = "http"
			checks = append(checks, onboardingCheck{
				ID:     "brain_http",
				Status: "ok",
				Label:  "Brain (OpenClaw HTTP)",
				Detail: "configured",
			})
			return resolved, checks
		}
		resolved = "mock"
		checks = append(checks, onboardingCheck{
			ID:     "brain_mock",
			Status: "warn",
			Label:  "Brain (mock)",
			Detail: "OpenClaw not configured.",
			Fix:    "Install OpenClaw, or set OPENCLAW_GATEWAY_TOKEN/OPENCLAW_HTTP_URL.",
		})
	default:
		resolved = "mock"
		checks = append(checks, onboardingCheck{
			ID:     "brain_mode",
			Status: "warn",
			Label:  "Brain",
			Detail: "unknown OPENCLAW_ADAPTER_MODE; using mock",
		})
	}

	return resolved, checks
}
