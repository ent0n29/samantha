package voice

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// NewFailoverProviderPair builds STT/TTS providers that prefer the primary backend
// and automatically switch to fallback when primary stream/session startup fails.
// Once fallback succeeds, it stays active until fallback fails; then primary is retried.
func NewFailoverProviderPair(
	primarySTT STTProvider,
	primaryTTS TTSProvider,
	fallbackSTT STTProvider,
	fallbackTTS TTSProvider,
	fallbackVoiceID string,
	fallbackModelID string,
) (STTProvider, TTSProvider) {
	state := &failoverState{}
	return &failoverSTTProvider{
			state:    state,
			primary:  primarySTT,
			fallback: fallbackSTT,
		}, &failoverTTSProvider{
			state:           state,
			primary:         primaryTTS,
			fallback:        fallbackTTS,
			fallbackVoiceID: strings.TrimSpace(fallbackVoiceID),
			fallbackModelID: strings.TrimSpace(fallbackModelID),
		}
}

type failoverState struct {
	fallbackActive atomic.Bool
}

func (s *failoverState) activateFallback() {
	s.fallbackActive.Store(true)
}

func (s *failoverState) deactivateFallback() {
	s.fallbackActive.Store(false)
}

func (s *failoverState) isFallbackActive() bool {
	return s.fallbackActive.Load()
}

type failoverSTTProvider struct {
	state    *failoverState
	primary  STTProvider
	fallback STTProvider
}

func (p *failoverSTTProvider) StartSession(ctx context.Context, sessionID string) (STTSession, <-chan STTEvent, error) {
	if p.state.isFallbackActive() {
		session, events, fbErr := p.fallback.StartSession(ctx, sessionID)
		if fbErr == nil {
			return session, events, nil
		}
		// Fallback failed after being active; try primary again.
		session, events, prErr := p.primary.StartSession(ctx, sessionID)
		if prErr == nil {
			p.state.deactivateFallback()
			return session, events, nil
		}
		return nil, nil, fmt.Errorf("stt fallback failed: %v; stt primary failed: %w", fbErr, prErr)
	}

	session, events, prErr := p.primary.StartSession(ctx, sessionID)
	if prErr == nil {
		return session, events, nil
	}

	session, events, fbErr := p.fallback.StartSession(ctx, sessionID)
	if fbErr != nil {
		return nil, nil, fmt.Errorf("stt primary failed: %v; stt fallback failed: %w", prErr, fbErr)
	}
	p.state.activateFallback()
	return session, events, nil
}

type failoverTTSProvider struct {
	state           *failoverState
	primary         TTSProvider
	fallback        TTSProvider
	fallbackVoiceID string
	fallbackModelID string
}

func (p *failoverTTSProvider) StartStream(
	ctx context.Context,
	voiceID, modelID string,
	settings TTSSettings,
) (TTSStream, error) {
	if p.state.isFallbackActive() {
		stream, fbErr := p.startFallbackStream(ctx, voiceID, modelID, settings)
		if fbErr == nil {
			return stream, nil
		}
		// Fallback failed after being active; try primary again.
		stream, prErr := p.primary.StartStream(ctx, voiceID, modelID, settings)
		if prErr == nil {
			p.state.deactivateFallback()
			return stream, nil
		}
		return nil, fmt.Errorf("tts fallback failed: %v; tts primary failed: %w", fbErr, prErr)
	}

	stream, prErr := p.primary.StartStream(ctx, voiceID, modelID, settings)
	if prErr == nil {
		return stream, nil
	}
	stream, fbErr := p.startFallbackStream(ctx, voiceID, modelID, settings)
	if fbErr != nil {
		return nil, fmt.Errorf("tts primary failed: %v; tts fallback failed: %w", prErr, fbErr)
	}
	p.state.activateFallback()
	return stream, nil
}

func (p *failoverTTSProvider) startFallbackStream(
	ctx context.Context,
	voiceID, modelID string,
	settings TTSSettings,
) (TTSStream, error) {
	fallbackVoiceID := voiceID
	if p.fallbackVoiceID != "" {
		fallbackVoiceID = p.fallbackVoiceID
	}
	fallbackModelID := modelID
	if p.fallbackModelID != "" {
		fallbackModelID = p.fallbackModelID
	}
	return p.fallback.StartStream(ctx, fallbackVoiceID, fallbackModelID, settings)
}
