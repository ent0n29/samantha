package httpapi

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type onboardingCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok|warn|error
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type onboardingStatusResponse struct {
	VoiceProvider  string            `json:"voice_provider"`
	BrainProvider  string            `json:"brain_provider"`
	UIAudioWorklet bool              `json:"ui_audio_worklet"`
	Checks         []onboardingCheck `json:"checks"`
}

func (s *Server) handleOnboardingStatus(w http.ResponseWriter, _ *http.Request) {
	voiceProvider := strings.ToLower(strings.TrimSpace(s.cfg.VoiceProvider))
	if voiceProvider == "" {
		voiceProvider = "auto"
	}

	brainProvider, brainChecks := s.brainChecks()
	checks := make([]onboardingCheck, 0, 12)
	checks = append(checks, onboardingCheck{
		ID:     "voice_provider",
		Status: "ok",
		Label:  "Voice backend",
		Detail: voiceProvider,
	})
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
		VoiceProvider:  voiceProvider,
		BrainProvider:  brainProvider,
		UIAudioWorklet: s.cfg.UIAudioWorklet,
		Checks:         checks,
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

	var resolved string
	checks := make([]onboardingCheck, 0, 3)

	switch mode {
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
			Fix:    "Install OpenClaw, or set OPENCLAW_HTTP_URL.",
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
