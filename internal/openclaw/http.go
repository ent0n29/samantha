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
	url          string
	client       *http.Client
	streamStrict bool
}

func NewHTTPAdapter(url string) *HTTPAdapter {
	return NewHTTPAdapterWithOptions(url, false)
}

func NewHTTPAdapterWithOptions(url string, streamStrict bool) *HTTPAdapter {
	return &HTTPAdapter{
		url:          strings.TrimSpace(url),
		streamStrict: streamStrict,
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
	if strings.Contains(ct, "text/event-stream") {
		return a.consumeSSE(res.Body, onDelta)
	}
	if strings.Contains(ct, "application/x-ndjson") || strings.Contains(ct, "application/ndjson") {
		return a.consumeNDJSON(res.Body, onDelta)
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

func (a *HTTPAdapter) consumeNDJSON(body io.Reader, onDelta DeltaHandler) (MessageResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var out strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		delta, ok, done, err := a.streamDelta(line)
		if err != nil {
			return MessageResponse{}, err
		}
		if done {
			return MessageResponse{Text: out.String()}, nil
		}
		if !ok {
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

func (a *HTTPAdapter) consumeSSE(body io.Reader, onDelta DeltaHandler) (MessageResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		out       strings.Builder
		dataLines []string
	)

	flushEvent := func() (done bool, err error) {
		if len(dataLines) == 0 {
			return false, nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]

		delta, ok, finished, err := a.streamDelta(payload)
		if err != nil {
			return false, err
		}
		if finished {
			return true, nil
		}
		if !ok {
			return false, nil
		}

		out.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			done, err := flushEvent()
			if err != nil {
				return MessageResponse{}, err
			}
			if done {
				return MessageResponse{Text: out.String()}, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			// SSE comment / keepalive.
			continue
		}

		field := line
		value := ""
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			field = line[:idx]
			value = line[idx+1:]
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		}

		switch field {
		case "data":
			dataLines = append(dataLines, value)
		default:
			// Ignore event/id/retry and unknown fields.
		}
	}

	done, err := flushEvent()
	if err != nil {
		return MessageResponse{}, err
	}
	if done {
		return MessageResponse{Text: out.String()}, nil
	}
	if err := scanner.Err(); err != nil {
		return MessageResponse{}, fmt.Errorf("stream read: %w", err)
	}
	return MessageResponse{Text: out.String()}, nil
}

func (a *HTTPAdapter) streamDelta(payload string) (delta string, ok bool, done bool, err error) {
	raw := payload
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", false, false, nil
	}
	if strings.EqualFold(p, "[DONE]") {
		return "", false, true, nil
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(p), &obj); err == nil {
		delta = strings.TrimSpace(extractText(obj))
		if delta == "" {
			return "", false, false, nil
		}
		return delta, true, false, nil
	}

	if a.streamStrict {
		return "", false, false, fmt.Errorf("invalid stream payload: %s", summarizePayload(p))
	}
	return raw, true, false, nil
}

func summarizePayload(p string) string {
	const maxLen = 200
	p = strings.TrimSpace(p)
	if len(p) <= maxLen {
		return p
	}
	return p[:maxLen] + "...(truncated)"
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
