package openclaw

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// CLIAdapter executes the OpenClaw CLI and extracts a textual reply.
type CLIAdapter struct {
	binaryPath     string
	thinking       string
	streaming      bool
	streamMinChars int
}

func NewCLIAdapter(binaryPath, thinking string, streaming bool, streamMinChars int) *CLIAdapter {
	return &CLIAdapter{
		binaryPath:     strings.TrimSpace(binaryPath),
		thinking:       normalizeThinkingLevel(thinking),
		streaming:      streaming,
		streamMinChars: normalizeStreamMinChars(streamMinChars),
	}
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
	args := a.buildArgs(buildPrompt(req), sessionID)
	if a.streaming {
		return a.streamResponseIncremental(ctx, args, onDelta)
	}
	return a.streamResponseBuffered(ctx, args, onDelta)
}

func normalizeThinkingLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "high":
		return "high"
	default:
		return "medium"
	}
}

func normalizeStreamMinChars(raw int) int {
	if raw <= 0 {
		return 24
	}
	if raw > 2048 {
		return 2048
	}
	return raw
}

func (a *CLIAdapter) buildArgs(prompt, sessionID string) []string {
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
		a.thinking,
	)
	return args
}

func (a *CLIAdapter) streamResponseBuffered(
	ctx context.Context,
	args []string,
	onDelta DeltaHandler,
) (MessageResponse, error) {
	cmd := exec.CommandContext(ctx, a.binaryPath, args...)
	var stdout strings.Builder
	var stderr strings.Builder
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

func (a *CLIAdapter) streamResponseIncremental(
	ctx context.Context,
	args []string,
	onDelta DeltaHandler,
) (MessageResponse, error) {
	cmd := exec.CommandContext(ctx, a.binaryPath, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return MessageResponse{}, fmt.Errorf("openclaw cli stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return MessageResponse{}, fmt.Errorf("openclaw cli stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return MessageResponse{}, fmt.Errorf("openclaw cli start: %w", err)
	}

	chunks := make(chan []byte, 24)
	readErrCh := make(chan error, 1)
	go streamReadChunks(stdoutPipe, chunks, readErrCh)

	var (
		stderrMu sync.Mutex
		stderrSB strings.Builder
	)
	stderrErrCh := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&lockedWriter{mu: &stderrMu, sb: &stderrSB}, stderrPipe)
		stderrErrCh <- copyErr
	}()

	var stdoutFull strings.Builder
	collector := newCLIStreamCollector(a.streamMinChars)

	for chunk := range chunks {
		stdoutFull.Write(chunk)
		if onDelta == nil {
			continue
		}
		for _, delta := range collector.ConsumeChunk(chunk) {
			if strings.TrimSpace(delta) == "" {
				continue
			}
			if err := onDelta(delta); err != nil {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = cmd.Wait()
				return MessageResponse{}, err
			}
		}
	}
	if readErr := <-readErrCh; readErr != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return MessageResponse{}, fmt.Errorf("openclaw cli stdout read: %w", readErr)
	}

	waitErr := cmd.Wait()
	if stderrErr := <-stderrErrCh; stderrErr != nil {
		return MessageResponse{}, fmt.Errorf("openclaw cli stderr read: %w", stderrErr)
	}

	stderrMu.Lock()
	stderrText := strings.TrimSpace(stderrSB.String())
	stderrMu.Unlock()
	stdoutText := stdoutFull.String()

	finalText := parseCLIReply(stdoutText)
	if finalText == "" {
		finalText = strings.TrimSpace(stdoutText)
	}

	remaining := collector.Finalize(finalText)
	if onDelta != nil {
		for _, delta := range remaining {
			if strings.TrimSpace(delta) == "" {
				continue
			}
			if err := onDelta(delta); err != nil {
				return MessageResponse{}, err
			}
		}
	}

	if waitErr != nil {
		if ctx.Err() != nil {
			return MessageResponse{}, ctx.Err()
		}
		errText := stderrText
		if errText == "" {
			errText = strings.TrimSpace(stdoutText)
		}
		if errText != "" {
			return MessageResponse{}, fmt.Errorf("openclaw cli failed: %w: %s", waitErr, errText)
		}
		return MessageResponse{}, fmt.Errorf("openclaw cli failed: %w", waitErr)
	}

	return MessageResponse{Text: finalText}, nil
}

func streamReadChunks(r io.Reader, chunks chan<- []byte, errCh chan<- error) {
	defer close(chunks)
	reader := bufio.NewReaderSize(r, 64*1024)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			out := make([]byte, n)
			copy(out, buf[:n])
			chunks <- out
		}
		if err != nil {
			if err == io.EOF {
				errCh <- nil
				return
			}
			errCh <- err
			return
		}
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	sb *strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.Write(p)
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
