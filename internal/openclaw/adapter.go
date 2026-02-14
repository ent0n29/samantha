package openclaw

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// MessageRequest is the normalized request sent to OpenClaw.
type MessageRequest struct {
	UserID        string   `json:"user_id"`
	SessionID     string   `json:"session_id"`
	TurnID        string   `json:"turn_id"`
	InputText     string   `json:"input_text"`
	MemoryContext []string `json:"memory_context,omitempty"`
	PersonaID     string   `json:"persona_id,omitempty"`
}

// MessageResponse is the final response after streaming deltas.
type MessageResponse struct {
	Text string `json:"text"`
}

// DeltaHandler receives streaming text fragments.
type DeltaHandler func(delta string) error

// Adapter bridges the companion runtime with OpenClaw reasoning.
type Adapter interface {
	StreamResponse(ctx context.Context, req MessageRequest, onDelta DeltaHandler) (MessageResponse, error)
}

// Config controls adapter construction.
type Config struct {
	Mode              string
	GatewayURL        string
	GatewayToken      string
	HTTPURL           string
	CLIPath           string
	CLIThinking       string
	CLIStreaming      bool
	CLIStreamMinChars int
	HTTPStreamStrict  bool
}

func NewAdapter(cfg Config) (Adapter, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "auto"
	}

	switch mode {
	case "auto":
		return newAutoAdapter(cfg), nil
	case "gateway":
		return NewGatewayAdapter(cfg.GatewayURL, cfg.GatewayToken, cfg.CLIThinking, cfg.CLIStreamMinChars)
	case "cli":
		if strings.TrimSpace(cfg.CLIPath) == "" {
			return nil, errors.New("openclaw CLI path is required for cli mode")
		}
		return NewCLIAdapter(cfg.CLIPath, cfg.CLIThinking, cfg.CLIStreaming, cfg.CLIStreamMinChars), nil
	case "http":
		if strings.TrimSpace(cfg.HTTPURL) == "" {
			return nil, errors.New("openclaw HTTP url is required for http mode")
		}
		return NewHTTPAdapterWithOptions(cfg.HTTPURL, cfg.HTTPStreamStrict), nil
	case "mock":
		return NewMockAdapter(), nil
	default:
		return nil, fmt.Errorf("unsupported openclaw adapter mode %q", cfg.Mode)
	}
}

func newAutoAdapter(cfg Config) Adapter {
	secondary := newAutoAdapterNoGateway(cfg)

	// Prefer the Gateway WS protocol when possible because it yields streaming assistant deltas.
	if strings.TrimSpace(cfg.GatewayToken) != "" {
		if gw, err := NewGatewayAdapter(cfg.GatewayURL, cfg.GatewayToken, cfg.CLIThinking, cfg.CLIStreamMinChars); err == nil {
			return NewFallbackAdapter(gw, secondary)
		}
	}

	return secondary
}

func newAutoAdapterNoGateway(cfg Config) Adapter {
	cliPath := strings.TrimSpace(cfg.CLIPath)
	if cliPath != "" {
		if _, err := exec.LookPath(cliPath); err == nil {
			// Fail fast when the CLI exists: silent fallbacks hide auth/provider issues and
			// make it hard to know whether you're running the real brain.
			return NewCLIAdapter(cliPath, cfg.CLIThinking, cfg.CLIStreaming, cfg.CLIStreamMinChars)
		}
	}

	httpURL := strings.TrimSpace(cfg.HTTPURL)
	if httpURL != "" {
		return NewHTTPAdapterWithOptions(httpURL, cfg.HTTPStreamStrict)
	}

	return NewMockAdapter()
}
