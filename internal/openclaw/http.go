package openclaw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPAdapter forwards requests to an OpenClaw-compatible HTTP endpoint.
type HTTPAdapter struct {
	url    string
	client *http.Client
}

func NewHTTPAdapter(url string) *HTTPAdapter {
	return &HTTPAdapter{
		url: strings.TrimSpace(url),
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (a *HTTPAdapter) StreamResponse(ctx context.Context, req MessageRequest, onDelta DeltaHandler) (MessageResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return MessageResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(payload))
	if err != nil {
		return MessageResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := a.client.Do(httpReq)
	if err != nil {
		return MessageResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return MessageResponse{}, fmt.Errorf("openclaw http status %d: %s", res.StatusCode, string(body))
	}

	ct := strings.ToLower(res.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") || strings.Contains(ct, "application/x-ndjson") {
		return a.consumeStreaming(res.Body, onDelta)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return MessageResponse{}, fmt.Errorf("read response: %w", err)
	}

	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		text := strings.TrimSpace(string(body))
		if text == "" {
			return MessageResponse{}, nil
		}
		if onDelta != nil {
			if err := onDelta(text); err != nil {
				return MessageResponse{}, err
			}
		}
		return MessageResponse{Text: text}, nil
	}

	text := extractText(obj)
	if text != "" && onDelta != nil {
		if err := onDelta(text); err != nil {
			return MessageResponse{}, err
		}
	}
	return MessageResponse{Text: text}, nil
}

func (a *HTTPAdapter) consumeStreaming(body io.Reader, onDelta DeltaHandler) (MessageResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var out strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}

		delta := line
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			delta = strings.TrimSpace(extractText(obj))
		}

		if delta == "" {
			continue
		}
		out.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return MessageResponse{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return MessageResponse{}, fmt.Errorf("stream read: %w", err)
	}

	return MessageResponse{Text: out.String()}, nil
}

func extractText(obj map[string]any) string {
	for _, k := range []string{"text", "delta", "output", "message"} {
		if v, ok := obj[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
