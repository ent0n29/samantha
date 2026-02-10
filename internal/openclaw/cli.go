package openclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CLIAdapter executes the OpenClaw CLI and extracts a textual reply.
type CLIAdapter struct {
	binaryPath string
}

func NewCLIAdapter(binaryPath string) *CLIAdapter {
	return &CLIAdapter{binaryPath: strings.TrimSpace(binaryPath)}
}

func (a *CLIAdapter) StreamResponse(
	ctx context.Context,
	req MessageRequest,
	onDelta DeltaHandler,
) (MessageResponse, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "samantha"
	}

	prompt := buildPrompt(req)
	args := []string{
		"agent",
		"--local",
		"--json",
		"--no-color",
	}

	if agentID := strings.TrimSpace(os.Getenv("OPENCLAW_AGENT_ID")); agentID != "" {
		args = append(args, "--agent", agentID)
	}

	args = append(args,
		"--session-id",
		sessionID,
		"--message",
		prompt,
		"--thinking",
		"high",
	)

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			// exec.CommandContext may surface "signal: killed" instead of context cancellation.
			return MessageResponse{}, ctx.Err()
		}
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = strings.TrimSpace(stdout.String())
		}
		if errText != "" {
			return MessageResponse{}, fmt.Errorf("openclaw cli failed: %w: %s", err, errText)
		}
		return MessageResponse{}, fmt.Errorf("openclaw cli failed: %w", err)
	}

	text := parseCLIReply(stdout.String())
	if text == "" {
		text = strings.TrimSpace(stdout.String())
	}
	if text != "" && onDelta != nil {
		if err := onDelta(text); err != nil {
			return MessageResponse{}, err
		}
	}

	return MessageResponse{Text: text}, nil
}

func buildPrompt(req MessageRequest) string {
	input := strings.TrimSpace(req.InputText)
	if input == "" {
		return ""
	}

	hasPersona := strings.TrimSpace(req.PersonaID) != ""
	hasMemory := len(req.MemoryContext) > 0
	if !hasPersona && !hasMemory {
		return input
	}

	var b strings.Builder
	if hasPersona {
		b.WriteString("Persona: ")
		b.WriteString(strings.TrimSpace(req.PersonaID))
		b.WriteString("\n")
	}
	if hasMemory {
		b.WriteString("Relevant conversation context:\n")
		for _, line := range req.MemoryContext {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("User message:\n")
	b.WriteString(input)
	return b.String()
}

func parseCLIReply(raw string) string {
	obj, ok := parseJSONObject(raw)
	if !ok {
		return ""
	}

	payloads := payloadArrayFrom(obj)
	if len(payloads) == 0 {
		return pickStringField(obj, "text", "output", "message")
	}

	parts := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		text := pickStringField(payload, "text")
		if text != "" {
			parts = append(parts, strings.TrimSpace(text))
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func payloadArrayFrom(obj map[string]any) []map[string]any {
	if direct := asObjectArray(obj["payloads"]); len(direct) > 0 {
		return direct
	}
	if result, ok := obj["result"].(map[string]any); ok {
		return asObjectArray(result["payloads"])
	}
	return nil
}

func asObjectArray(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

func pickStringField(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func parseJSONObject(raw string) (map[string]any, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		return obj, true
	}

	// Many CLIs emit logs before JSON. Parse from the last JSON-looking block.
	start := strings.LastIndex(raw, "\n{")
	if start >= 0 {
		start++
		if err := json.Unmarshal([]byte(raw[start:]), &obj); err == nil {
			return obj, true
		}
	}

	brace := strings.LastIndex(raw, "{")
	if brace >= 0 {
		if err := json.Unmarshal([]byte(raw[brace:]), &obj); err == nil {
			return obj, true
		}
	}

	return nil, false
}
