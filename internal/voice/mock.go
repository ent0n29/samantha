package voice

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

// MockProvider is a local fallback provider used when ElevenLabs is not configured.
type MockProvider struct{}

func NewMockProvider() *MockProvider { return &MockProvider{} }

func (p *MockProvider) StartSession(_ context.Context, _ string) (STTSession, <-chan STTEvent, error) {
	events := make(chan STTEvent, 64)
	s := &mockSTTSession{events: events}
	return s, events, nil
}

func (p *MockProvider) StartStream(_ context.Context, _ string, _ string, _ TTSSettings) (TTSStream, error) {
	events := make(chan TTSEvent, 128)
	return &mockTTSStream{events: events}, nil
}

type mockSTTSession struct {
	mu        sync.Mutex
	events    chan STTEvent
	chunks    int
	closed    bool
	lastInput string
}

func (s *mockSTTSession) SendAudioChunk(_ context.Context, audioBase64 string, _ int, commit bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.chunks++
	if audioBase64 != "" {
		s.lastInput = audioBase64
		s.events <- STTEvent{Type: STTEventPartial, Text: "...", Confidence: 0.5, Timestamp: time.Now().UnixMilli()}
	}
	if commit || s.chunks%8 == 0 {
		text := "simulated voice input"
		if strings.TrimSpace(s.lastInput) == "" {
			text = ""
		}
		s.events <- STTEvent{Type: STTEventCommitted, Text: text, Confidence: 0.7, Source: "mock_commit", Timestamp: time.Now().UnixMilli()}
	}
	return nil
}

func (s *mockSTTSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

type mockTTSStream struct {
	mu     sync.Mutex
	events chan TTSEvent
	closed bool
}

func (s *mockTTSStream) SendText(_ context.Context, text string, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	s.events <- TTSEvent{Type: TTSEventAudio, AudioBase64: encoded, Format: "mock_text_bytes"}
	return nil
}

func (s *mockTTSStream) CloseInput(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.events <- TTSEvent{Type: TTSEventFinal}
	return nil
}

func (s *mockTTSStream) Events() <-chan TTSEvent { return s.events }

func (s *mockTTSStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}
