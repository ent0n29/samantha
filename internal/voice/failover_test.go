package voice

import (
	"context"
	"errors"
	"testing"
)

func TestFailoverProviderPairSwitchesToFallbackAndSticks(t *testing.T) {
	ctx := context.Background()
	primaryErr := errors.New("primary unavailable")

	primarySTT := &stubSTTProvider{
		startSession: func(context.Context, string) (STTSession, <-chan STTEvent, error) {
			return nil, nil, primaryErr
		},
	}
	fallbackSTT := &stubSTTProvider{
		startSession: func(context.Context, string) (STTSession, <-chan STTEvent, error) {
			return &stubSTTSession{}, make(chan STTEvent), nil
		},
	}
	primaryTTS := &stubTTSProvider{
		startStream: func(context.Context, string, string, TTSSettings) (TTSStream, error) {
			return nil, primaryErr
		},
	}
	fallbackTTS := &stubTTSProvider{
		startStream: func(context.Context, string, string, TTSSettings) (TTSStream, error) {
			return &stubTTSStream{}, nil
		},
	}

	stt, tts := NewFailoverProviderPair(primarySTT, primaryTTS, fallbackSTT, fallbackTTS, "af_heart", "kokoro")

	if _, _, err := stt.StartSession(ctx, "session-1"); err != nil {
		t.Fatalf("StartSession() unexpected error = %v", err)
	}
	if _, _, err := stt.StartSession(ctx, "session-2"); err != nil {
		t.Fatalf("StartSession() on fallback unexpected error = %v", err)
	}
	if _, err := tts.StartStream(ctx, "x", "y", TTSSettings{}); err != nil {
		t.Fatalf("StartStream() unexpected error = %v", err)
	}
	if _, err := tts.StartStream(ctx, "x", "y", TTSSettings{}); err != nil {
		t.Fatalf("StartStream() on fallback unexpected error = %v", err)
	}

	if primarySTT.calls != 1 {
		t.Fatalf("primary STT calls = %d, want 1", primarySTT.calls)
	}
	if fallbackSTT.calls != 2 {
		t.Fatalf("fallback STT calls = %d, want 2", fallbackSTT.calls)
	}
	if primaryTTS.calls != 0 {
		t.Fatalf("primary TTS calls = %d, want 0 once fallback active", primaryTTS.calls)
	}
	if fallbackTTS.calls != 2 {
		t.Fatalf("fallback TTS calls = %d, want 2", fallbackTTS.calls)
	}
}

func TestFailoverProviderPairMapsFallbackVoiceAndModel(t *testing.T) {
	ctx := context.Background()
	primaryErr := errors.New("quota exceeded")

	primarySTT := &stubSTTProvider{
		startSession: func(context.Context, string) (STTSession, <-chan STTEvent, error) {
			return &stubSTTSession{}, make(chan STTEvent), nil
		},
	}
	fallbackSTT := &stubSTTProvider{
		startSession: func(context.Context, string) (STTSession, <-chan STTEvent, error) {
			return &stubSTTSession{}, make(chan STTEvent), nil
		},
	}

	var seenVoice string
	var seenModel string
	primaryTTS := &stubTTSProvider{
		startStream: func(context.Context, string, string, TTSSettings) (TTSStream, error) {
			return nil, primaryErr
		},
	}
	fallbackTTS := &stubTTSProvider{
		startStream: func(_ context.Context, voiceID, modelID string, _ TTSSettings) (TTSStream, error) {
			seenVoice = voiceID
			seenModel = modelID
			return &stubTTSStream{}, nil
		},
	}

	_, tts := NewFailoverProviderPair(primarySTT, primaryTTS, fallbackSTT, fallbackTTS, "af_heart", "kokoro")

	if _, err := tts.StartStream(ctx, "eleven_voice", "eleven_model", TTSSettings{}); err != nil {
		t.Fatalf("StartStream() unexpected error = %v", err)
	}
	if seenVoice != "af_heart" {
		t.Fatalf("fallback voice = %q, want %q", seenVoice, "af_heart")
	}
	if seenModel != "kokoro" {
		t.Fatalf("fallback model = %q, want %q", seenModel, "kokoro")
	}
}

func TestFailoverProviderPairReturnsCombinedErrorWhenBothFail(t *testing.T) {
	ctx := context.Background()
	primaryErr := errors.New("primary down")
	fallbackErr := errors.New("fallback down")

	primarySTT := &stubSTTProvider{
		startSession: func(context.Context, string) (STTSession, <-chan STTEvent, error) {
			return nil, nil, primaryErr
		},
	}
	fallbackSTT := &stubSTTProvider{
		startSession: func(context.Context, string) (STTSession, <-chan STTEvent, error) {
			return nil, nil, fallbackErr
		},
	}
	primaryTTS := &stubTTSProvider{
		startStream: func(context.Context, string, string, TTSSettings) (TTSStream, error) {
			return nil, primaryErr
		},
	}
	fallbackTTS := &stubTTSProvider{
		startStream: func(context.Context, string, string, TTSSettings) (TTSStream, error) {
			return nil, fallbackErr
		},
	}

	stt, tts := NewFailoverProviderPair(primarySTT, primaryTTS, fallbackSTT, fallbackTTS, "af_heart", "kokoro")
	if _, _, err := stt.StartSession(ctx, "session-1"); err == nil {
		t.Fatalf("StartSession() expected error when both providers fail")
	}
	if _, err := tts.StartStream(ctx, "voice", "model", TTSSettings{}); err == nil {
		t.Fatalf("StartStream() expected error when both providers fail")
	}
}

type stubSTTProvider struct {
	calls        int
	startSession func(ctx context.Context, sessionID string) (STTSession, <-chan STTEvent, error)
}

func (p *stubSTTProvider) StartSession(ctx context.Context, sessionID string) (STTSession, <-chan STTEvent, error) {
	p.calls++
	return p.startSession(ctx, sessionID)
}

type stubTTSProvider struct {
	calls       int
	startStream func(ctx context.Context, voiceID, modelID string, settings TTSSettings) (TTSStream, error)
}

func (p *stubTTSProvider) StartStream(
	ctx context.Context,
	voiceID, modelID string,
	settings TTSSettings,
) (TTSStream, error) {
	p.calls++
	return p.startStream(ctx, voiceID, modelID, settings)
}

type stubSTTSession struct{}

func (s *stubSTTSession) SendAudioChunk(context.Context, string, int, bool) error { return nil }
func (s *stubSTTSession) Close() error                                            { return nil }

type stubTTSStream struct{}

func (s *stubTTSStream) SendText(context.Context, string, bool) error { return nil }
func (s *stubTTSStream) CloseInput(context.Context) error             { return nil }
func (s *stubTTSStream) Events() <-chan TTSEvent                      { return make(chan TTSEvent) }
func (s *stubTTSStream) Close() error                                 { return nil }
