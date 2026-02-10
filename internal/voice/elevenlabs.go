package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/antoniostano/samantha/internal/reliability"
	"github.com/gorilla/websocket"
)

type ElevenLabsConfig struct {
	APIKey              string
	WSBaseURL           string
	STTModelID          string
	DefaultOutputFormat string
}

type ElevenLabsProvider struct {
	cfg ElevenLabsConfig
}

func NewElevenLabsProvider(cfg ElevenLabsConfig) *ElevenLabsProvider {
	if strings.TrimSpace(cfg.WSBaseURL) == "" {
		cfg.WSBaseURL = "wss://api.elevenlabs.io"
	}
	if strings.TrimSpace(cfg.STTModelID) == "" {
		cfg.STTModelID = "scribe_v1"
	}
	if strings.TrimSpace(cfg.DefaultOutputFormat) == "" {
		cfg.DefaultOutputFormat = "mp3_44100_128"
	}
	return &ElevenLabsProvider{cfg: cfg}
}

func (p *ElevenLabsProvider) StartSession(ctx context.Context, _ string) (STTSession, <-chan STTEvent, error) {
	u, err := url.Parse(strings.TrimRight(p.cfg.WSBaseURL, "/") + "/v1/speech-to-text/realtime")
	if err != nil {
		return nil, nil, err
	}
	q := u.Query()
	q.Set("model_id", p.cfg.STTModelID)
	q.Set("commit_strategy", "vad")
	q.Set("include_timestamps", "true")
	u.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("xi-api-key", p.cfg.APIKey)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		return nil, nil, fmt.Errorf("dial stt websocket: %w", err)
	}

	events := make(chan STTEvent, 256)
	s := &elevenSTTSession{conn: conn, events: events}
	go s.readLoop()
	return s, events, nil
}

func (p *ElevenLabsProvider) StartStream(ctx context.Context, voiceID, modelID string, settings TTSSettings) (TTSStream, error) {
	if strings.TrimSpace(voiceID) == "" {
		return nil, fmt.Errorf("voice_id is required")
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "eleven_multilingual_v2"
	}

	stability := settings.Stability
	if stability <= 0 {
		stability = 0.42
	}
	if stability < 0 {
		stability = 0
	} else if stability > 1 {
		stability = 1
	}

	similarity := settings.SimilarityBoost
	if similarity <= 0 {
		similarity = 0.85
	}
	if similarity < 0 {
		similarity = 0
	} else if similarity > 1 {
		similarity = 1
	}

	speed := settings.Speed
	if speed <= 0 {
		speed = 1.0
	}
	if speed < 0.7 {
		speed = 0.7
	} else if speed > 1.2 {
		speed = 1.2
	}

	u, err := url.Parse(strings.TrimRight(p.cfg.WSBaseURL, "/") + "/v1/text-to-speech/" + url.PathEscape(voiceID) + "/stream-input")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("model_id", modelID)
	q.Set("output_format", p.cfg.DefaultOutputFormat)
	q.Set("auto_mode", "true")
	u.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("xi-api-key", p.cfg.APIKey)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		return nil, fmt.Errorf("dial tts websocket: %w", err)
	}

	s := &elevenTTSStream{conn: conn, events: make(chan TTSEvent, 512)}
	go s.readLoop()
	// Prime the stream as documented for TTS websocket flows.
	_ = s.writeJSON(map[string]any{
		"text": " ",
		"voice_settings": map[string]any{
			"stability":        stability,
			"similarity_boost": similarity,
			"speed":            speed,
		},
	})
	return s, nil
}

type elevenSTTSession struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
	events    chan STTEvent
}

func (s *elevenSTTSession) SendAudioChunk(_ context.Context, audioBase64 string, sampleRate int, commit bool) error {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	payload := map[string]any{
		"message_type":  "input_audio_chunk",
		"audio_base_64": audioBase64,
		"commit":        commit,
		"sample_rate":   sampleRate,
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteJSON(payload)
}

func (s *elevenSTTSession) readLoop() {
	defer s.safeClose()
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		messageType := asString(raw["message_type"])
		switch messageType {
		case "partial_transcript":
			s.events <- STTEvent{Type: STTEventPartial, Text: asString(raw["text"]), Timestamp: time.Now().UnixMilli()}
		case "committed_transcript", "committed_transcript_with_timestamps":
			s.events <- STTEvent{Type: STTEventCommitted, Text: asString(raw["text"]), Timestamp: time.Now().UnixMilli()}
		case "session_started":
			// ignore control event
		case "", "input_audio_chunk":
			// ignore
		default:
			s.events <- STTEvent{
				Type:      STTEventError,
				Code:      messageType,
				Detail:    asString(raw["error"]),
				Retryable: reliability.IsRetryableRealtimeMessageType(messageType),
				Timestamp: time.Now().UnixMilli(),
			}
		}
	}
}

func (s *elevenSTTSession) Close() error {
	var retErr error
	s.closeOnce.Do(func() {
		retErr = s.conn.Close()
		close(s.events)
	})
	return retErr
}

func (s *elevenSTTSession) safeClose() {
	s.closeOnce.Do(func() {
		_ = s.conn.Close()
		close(s.events)
	})
}

type elevenTTSStream struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
	events    chan TTSEvent
}

func (s *elevenTTSStream) SendText(_ context.Context, text string, tryTrigger bool) error {
	payload := map[string]any{
		"text":                   text,
		"try_trigger_generation": tryTrigger,
	}
	return s.writeJSON(payload)
}

func (s *elevenTTSStream) CloseInput(_ context.Context) error {
	return s.writeJSON(map[string]any{"text": ""})
}

func (s *elevenTTSStream) Events() <-chan TTSEvent { return s.events }

func (s *elevenTTSStream) Close() error {
	var retErr error
	s.closeOnce.Do(func() {
		retErr = s.conn.Close()
		close(s.events)
	})
	return retErr
}

func (s *elevenTTSStream) writeJSON(payload map[string]any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteJSON(payload)
}

func (s *elevenTTSStream) readLoop() {
	defer s.safeClose()
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}

		if audio := asString(raw["audio"]); audio != "" {
			s.events <- TTSEvent{Type: TTSEventAudio, AudioBase64: audio, Format: "base64_audio"}
		}
		if asBool(raw["isFinal"]) || asBool(raw["is_final"]) {
			s.events <- TTSEvent{Type: TTSEventFinal}
		}
		if errMsg := asString(raw["error"]); errMsg != "" {
			code := asString(raw["message_type"])
			s.events <- TTSEvent{Type: TTSEventError, Code: code, Detail: errMsg, Retryable: reliability.IsRetryableRealtimeMessageType(code)}
		}
	}
}

func (s *elevenTTSStream) safeClose() {
	s.closeOnce.Do(func() {
		_ = s.conn.Close()
		close(s.events)
	})
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
