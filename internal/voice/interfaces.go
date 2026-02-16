package voice

import "context"

type STTEventType string

const (
	STTEventPartial   STTEventType = "partial"
	STTEventCommitted STTEventType = "committed"
	STTEventError     STTEventType = "error"
)

type STTEvent struct {
	Type       STTEventType
	Text       string
	Confidence float64
	Source     string
	Code       string
	Detail     string
	Retryable  bool
	Timestamp  int64
}

type STTSession interface {
	SendAudioChunk(ctx context.Context, audioBase64 string, sampleRate int, commit bool) error
	Close() error
}

type STTProvider interface {
	StartSession(ctx context.Context, sessionID string) (STTSession, <-chan STTEvent, error)
}

type TTSEventType string

const (
	TTSEventAudio TTSEventType = "audio"
	TTSEventFinal TTSEventType = "final"
	TTSEventError TTSEventType = "error"
)

type TTSEvent struct {
	Type        TTSEventType
	AudioBase64 string
	Format      string
	Code        string
	Detail      string
	Retryable   bool
}

type TTSSettings struct {
	Stability       float64
	SimilarityBoost float64
	Speed           float64
}

type TTSStream interface {
	SendText(ctx context.Context, text string, tryTrigger bool) error
	CloseInput(ctx context.Context) error
	Events() <-chan TTSEvent
	Close() error
}

type TTSProvider interface {
	StartStream(ctx context.Context, voiceID, modelID string, settings TTSSettings) (TTSStream, error)
}
