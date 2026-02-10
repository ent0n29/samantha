package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antoniostano/samantha/internal/memory"
	"github.com/antoniostano/samantha/internal/observability"
	"github.com/antoniostano/samantha/internal/openclaw"
	"github.com/antoniostano/samantha/internal/policy"
	"github.com/antoniostano/samantha/internal/protocol"
	"github.com/antoniostano/samantha/internal/reliability"
	"github.com/antoniostano/samantha/internal/session"
)

type PersonaProfile struct {
	ID           string
	DisplayName  string
	SystemStyle  string
	VoiceID      string
	ModelID      string
	SpeakingRate float64
	Warmth       float64
	VerbosityCap int
}

type Orchestrator struct {
	sessions      *session.Manager
	adapter       openclaw.Adapter
	memoryStore   memory.Store
	sttProvider   STTProvider
	ttsProvider   TTSProvider
	metrics       *observability.Metrics
	firstAudioSLO time.Duration
	defaultVoice  string
	defaultModel  string
	profiles      map[string]PersonaProfile
}

func NewOrchestrator(
	sessions *session.Manager,
	adapter openclaw.Adapter,
	memoryStore memory.Store,
	sttProvider STTProvider,
	ttsProvider TTSProvider,
	metrics *observability.Metrics,
	firstAudioSLO time.Duration,
	defaultVoice string,
	defaultModel string,
) *Orchestrator {
	profiles := map[string]PersonaProfile{
		"warm": {
			ID:           "warm",
			DisplayName:  "Warm",
			SystemStyle:  "empathetic, conversational, supportive",
			VoiceID:      defaultVoice,
			ModelID:      defaultModel,
			SpeakingRate: 0.97,
			Warmth:       0.9,
			VerbosityCap: 350,
		},
		"professional": {
			ID:           "professional",
			DisplayName:  "Professional",
			SystemStyle:  "clear, factual, concise",
			VoiceID:      defaultVoice,
			ModelID:      defaultModel,
			SpeakingRate: 0.98,
			Warmth:       0.4,
			VerbosityCap: 280,
		},
		"concise": {
			ID:           "concise",
			DisplayName:  "Concise",
			SystemStyle:  "brief, high-signal, direct",
			VoiceID:      defaultVoice,
			ModelID:      defaultModel,
			SpeakingRate: 1.05,
			Warmth:       0.3,
			VerbosityCap: 180,
		},
	}

	return &Orchestrator{
		sessions:      sessions,
		adapter:       adapter,
		memoryStore:   memoryStore,
		sttProvider:   sttProvider,
		ttsProvider:   ttsProvider,
		metrics:       metrics,
		firstAudioSLO: firstAudioSLO,
		defaultVoice:  defaultVoice,
		defaultModel:  defaultModel,
		profiles:      profiles,
	}
}

// PreviewTTS synthesizes a short standalone utterance for voice auditioning.
// This bypasses OpenClaw and memory; it only uses the configured TTS provider.
func (o *Orchestrator) PreviewTTS(ctx context.Context, voiceID, modelID, personaID, text string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	profile := o.profileForPersona(personaID)
	if strings.TrimSpace(voiceID) != "" {
		profile.VoiceID = strings.TrimSpace(voiceID)
	}
	if strings.TrimSpace(modelID) != "" {
		profile.ModelID = strings.TrimSpace(modelID)
	}

	if strings.TrimSpace(profile.VoiceID) == "" {
		return nil, "", fmt.Errorf("voice_id is required")
	}
	if strings.TrimSpace(profile.ModelID) == "" {
		profile.ModelID = o.defaultModel
	}
	if strings.TrimSpace(text) == "" {
		text = "Hi. I'm here with you."
	}

	settings := ttsSettingsForProfile(profile)
	stream, err := o.ttsProvider.StartStream(ctx, profile.VoiceID, profile.ModelID, settings)
	if err != nil {
		return nil, "", err
	}
	defer stream.Close()

	if err := stream.SendText(ctx, text, true); err != nil {
		_ = stream.CloseInput(ctx)
		return nil, "", err
	}
	_ = stream.CloseInput(ctx)

	var out bytes.Buffer
	var format string
	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case evt, ok := <-stream.Events():
			if !ok {
				return out.Bytes(), strings.TrimSpace(format), nil
			}
			switch evt.Type {
			case TTSEventAudio:
				if format == "" && strings.TrimSpace(evt.Format) != "" {
					format = strings.TrimSpace(evt.Format)
				}
				if strings.TrimSpace(evt.AudioBase64) == "" {
					continue
				}
				chunk, err := base64.StdEncoding.DecodeString(evt.AudioBase64)
				if err != nil {
					return nil, "", fmt.Errorf("decode audio chunk: %w", err)
				}
				_, _ = out.Write(chunk)
			case TTSEventFinal:
				return out.Bytes(), strings.TrimSpace(format), nil
			case TTSEventError:
				return nil, "", fmt.Errorf("tts error: %s %s", strings.TrimSpace(evt.Code), strings.TrimSpace(evt.Detail))
			}
		}
	}
}

// RunConnection drives a session lifecycle for one websocket connection.
func (o *Orchestrator) RunConnection(ctx context.Context, s *session.Session, inbound <-chan any, outbound chan<- any) error {
	sttSession, sttEvents, err := o.sttProvider.StartSession(ctx, s.ID)
	if err != nil {
		o.send(outbound, protocol.ErrorEvent{
			Type:      protocol.TypeErrorEvent,
			SessionID: s.ID,
			Code:      "stt_connect_failed",
			Source:    "stt",
			Retryable: reliability.IsRetryableHTTPStatus(503),
			Detail:    err.Error(),
		})
		return err
	}
	defer sttSession.Close()

	var (
		turnMu       sync.Mutex
		turnCancel   context.CancelFunc
		activeTurnID string
		activeToken  int64
		nextToken    int64
		lastSampleHz = 16000

		wakewordEnabled    bool
		manualArmUntil     time.Time
		awaitingQueryUntil time.Time
	)

	cancelActiveTurn := func(reason string) {
		turnMu.Lock()
		cancel := turnCancel
		turnID := activeTurnID
		turnCancel = nil
		activeTurnID = ""
		turnMu.Unlock()

		if cancel != nil {
			cancel()
			o.send(outbound, protocol.AssistantTurnEnd{
				Type:      protocol.TypeAssistantTurnEnd,
				SessionID: s.ID,
				TurnID:    turnID,
				Reason:    reason,
			})
		}
	}

	for {
		select {
		case <-ctx.Done():
			cancelActiveTurn("connection_closed")
			return nil
		case msg, ok := <-inbound:
			if !ok {
				cancelActiveTurn("connection_closed")
				return nil
			}
			switch m := msg.(type) {
			case protocol.ClientAudioChunk:
				if m.SampleRate > 0 {
					lastSampleHz = m.SampleRate
				}
				_ = o.sessions.Touch(s.ID)
				if err := sttSession.SendAudioChunk(ctx, m.PCM16Base64, m.SampleRate, false); err != nil {
					o.send(outbound, protocol.ErrorEvent{
						Type:      protocol.TypeErrorEvent,
						SessionID: s.ID,
						Code:      "stt_send_audio_failed",
						Source:    "stt",
						Retryable: true,
						Detail:    err.Error(),
					})
				}
			case protocol.ClientControl:
				_ = o.sessions.Touch(s.ID)
				switch m.Action {
				case "interrupt":
					_ = o.sessions.Interrupt(s.ID)
					cancelActiveTurn("interrupted")
				case "stop":
					_ = sttSession.SendAudioChunk(ctx, "", lastSampleHz, true)
				case "wakeword_on":
					wakewordEnabled = true
					manualArmUntil = time.Time{}
					awaitingQueryUntil = time.Time{}
				case "wakeword_off":
					wakewordEnabled = false
					manualArmUntil = time.Time{}
					awaitingQueryUntil = time.Time{}
				case "manual_arm":
					manualArmUntil = time.Now().Add(12 * time.Second)
					awaitingQueryUntil = time.Time{}
				case "start", "mute", "unmute":
					// no-op currently
				}
			}
		case evt, ok := <-sttEvents:
			if !ok {
				cancelActiveTurn("stt_closed")
				return nil
			}

			switch evt.Type {
			case STTEventPartial:
				o.send(outbound, protocol.STTPartial{
					Type:       protocol.TypeSTTPartial,
					SessionID:  s.ID,
					Text:       evt.Text,
					Confidence: evt.Confidence,
					TSMs:       evt.Timestamp,
				})
			case STTEventCommitted:
				committedText := strings.TrimSpace(evt.Text)
				if committedText == "" {
					continue
				}

				o.send(outbound, protocol.STTCommitted{
					Type:      protocol.TypeSTTCommitted,
					SessionID: s.ID,
					Text:      committedText,
					TSMs:      evt.Timestamp,
				})

				now := time.Now()
				if !manualArmUntil.IsZero() && now.After(manualArmUntil) {
					manualArmUntil = time.Time{}
				}
				if !awaitingQueryUntil.IsZero() && now.After(awaitingQueryUntil) {
					awaitingQueryUntil = time.Time{}
				}

				if wakewordEnabled {
					// Hands-free: require wake word unless the user explicitly armed a push-to-talk fallback.
					manualArmed := !manualArmUntil.IsZero()
					awaitingQuery := !awaitingQueryUntil.IsZero()

					if awaitingQuery && !manualArmed {
						hit, remainder := detectWakeWordCommitted(committedText)
						if hit && strings.TrimSpace(remainder) == "" {
							// The user repeated the wake word; keep the window open.
							awaitingQueryUntil = now.Add(8 * time.Second)
							o.send(outbound, protocol.SystemEvent{
								Type:      protocol.TypeSystemEvent,
								SessionID: s.ID,
								Code:      "wake_word",
								Detail:    "samantha",
							})
							continue
						}
						awaitingQueryUntil = time.Time{}
						if hit && strings.TrimSpace(remainder) != "" {
							committedText = remainder
						}
					} else if manualArmed {
						manualArmUntil = time.Time{}
						hit, remainder := detectWakeWordCommitted(committedText)
						if hit && strings.TrimSpace(remainder) != "" {
							committedText = remainder
						}
					} else {
						hit, remainder := detectWakeWordCommitted(committedText)
						if !hit {
							continue
						}
						if strings.TrimSpace(remainder) == "" {
							awaitingQueryUntil = now.Add(8 * time.Second)
							o.send(outbound, protocol.SystemEvent{
								Type:      protocol.TypeSystemEvent,
								SessionID: s.ID,
								Code:      "wake_word",
								Detail:    "samantha",
							})
							continue
						}
						committedText = remainder
					}

					committedText = strings.TrimSpace(committedText)
					if committedText == "" {
						continue
					}
				}

				// V0 is intentionally half-duplex: ignore fresh commits while assistant is talking.
				turnMu.Lock()
				busy := turnCancel != nil
				turnMu.Unlock()
				if busy {
					// User spoke while the assistant was still responding. Treat this as a barge-in:
					// cancel the current turn and pivot immediately.
					_ = o.sessions.Interrupt(s.ID)
					cancelActiveTurn("barge_in")
				}

				turnID := uuid.NewString()
				_ = o.sessions.StartTurn(s.ID, turnID)
				turnCtx, cancel := context.WithCancel(ctx)

				turnMu.Lock()
				nextToken++
				token := nextToken
				turnCancel = cancel
				activeTurnID = turnID
				activeToken = token
				turnMu.Unlock()

				go func(turnText, turnID string, token int64) {
					defer func() {
						turnMu.Lock()
						if activeToken == token {
							turnCancel = nil
							activeTurnID = ""
							activeToken = 0
						}
						turnMu.Unlock()
					}()

					if err := o.runAssistantTurn(turnCtx, *s, turnText, turnID, outbound); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						o.send(outbound, protocol.ErrorEvent{
							Type:      protocol.TypeErrorEvent,
							SessionID: s.ID,
							Code:      "assistant_turn_failed",
							Source:    "orchestrator",
							Retryable: false,
							Detail:    err.Error(),
						})
					}
				}(committedText, turnID, token)
			case STTEventError:
				if evt.Code == "commit_throttled" {
					// Non-fatal: happens when commit is requested with too little uncommitted audio.
					continue
				}
				o.metrics.ProviderErrors.WithLabelValues("elevenlabs_stt", evt.Code).Inc()
				o.send(outbound, protocol.ErrorEvent{
					Type:      protocol.TypeErrorEvent,
					SessionID: s.ID,
					Code:      evt.Code,
					Source:    "stt",
					Retryable: evt.Retryable,
					Detail:    evt.Detail,
				})
			}
		}
	}
}

func (o *Orchestrator) runAssistantTurn(ctx context.Context, s session.Session, userText, turnID string, outbound chan<- any) error {
	start := time.Now()
	profile := o.profileForSession(s)

	redactedUserText, userChanged := policy.RedactPII(userText)
	_ = o.memoryStore.SaveTurn(ctx, memory.TurnRecord{
		ID:          uuid.NewString(),
		UserID:      s.UserID,
		SessionID:   s.ID,
		Role:        "user",
		Content:     redactedUserText,
		PIIRedacted: userChanged,
		CreatedAt:   time.Now().UTC(),
	})

	recent, _ := o.memoryStore.RecentContext(ctx, s.UserID, 8)
	contextLines := make([]string, 0, len(recent))
	for _, r := range recent {
		contextLines = append(contextLines, fmt.Sprintf("%s: %s", r.Role, r.Content))
	}

	settings := ttsSettingsForProfile(profile)
	ttsStream, err := o.ttsProvider.StartStream(ctx, profile.VoiceID, profile.ModelID, settings)
	if err != nil {
		return fmt.Errorf("start tts stream: %w", err)
	}
	defer ttsStream.Close()

	firstAudioObserved := false
	ttsDone := make(chan struct{})
	go func() {
		defer close(ttsDone)
		seq := 0
		for {
			select {
			case <-ctx.Done():
				// Stop forwarding audio immediately on interruption; providers may still emit
				// buffered chunks but the client should pivot to the new user utterance.
				return
			case evt, ok := <-ttsStream.Events():
				if !ok {
					return
				}
				switch evt.Type {
				case TTSEventAudio:
					if !firstAudioObserved {
						firstAudioObserved = true
						o.metrics.ObserveFirstAudioLatency(time.Since(start))
					}
					seq++
					o.send(outbound, protocol.AssistantAudioChunk{
						Type:        protocol.TypeAssistantAudio,
						SessionID:   s.ID,
						TurnID:      turnID,
						Seq:         seq,
						Format:      evt.Format,
						AudioBase64: evt.AudioBase64,
					})
				case TTSEventError:
					o.metrics.ProviderErrors.WithLabelValues("elevenlabs_tts", evt.Code).Inc()
					o.send(outbound, protocol.ErrorEvent{
						Type:      protocol.TypeErrorEvent,
						SessionID: s.ID,
						Code:      evt.Code,
						Source:    "tts",
						Retryable: evt.Retryable,
						Detail:    evt.Detail,
					})
				case TTSEventFinal:
					return
				}
			}
		}
	}()

	var assistantOut string
	res, err := o.adapter.StreamResponse(ctx, openclaw.MessageRequest{
		UserID:        s.UserID,
		SessionID:     s.ID,
		TurnID:        turnID,
		InputText:     userText,
		MemoryContext: contextLines,
		PersonaID:     profile.ID,
	}, func(delta string) error {
		assistantOut += delta
		o.send(outbound, protocol.AssistantTextDelta{
			Type:      protocol.TypeAssistantTextDelta,
			SessionID: s.ID,
			TurnID:    turnID,
			TextDelta: delta,
		})
		return ttsStream.SendText(ctx, delta, true)
	})
	if err != nil {
		_ = ttsStream.CloseInput(ctx)
		return fmt.Errorf("stream response: %w", err)
	}
	if assistantOut == "" {
		assistantOut = res.Text
		if assistantOut != "" {
			_ = ttsStream.SendText(ctx, assistantOut, true)
		}
	}
	_ = ttsStream.CloseInput(ctx)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ttsDone:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("tts finalization timeout")
	}

	redactedAssistantText, assistantChanged := policy.RedactPII(assistantOut)
	_ = o.memoryStore.SaveTurn(ctx, memory.TurnRecord{
		ID:          uuid.NewString(),
		UserID:      s.UserID,
		SessionID:   s.ID,
		Role:        "assistant",
		Content:     redactedAssistantText,
		PIIRedacted: assistantChanged,
		CreatedAt:   time.Now().UTC(),
	})

	o.send(outbound, protocol.AssistantTurnEnd{
		Type:      protocol.TypeAssistantTurnEnd,
		SessionID: s.ID,
		TurnID:    turnID,
		Reason:    "completed",
	})

	if o.firstAudioSLO > 0 && firstAudioObserved {
		lat := time.Since(start)
		if lat > o.firstAudioSLO {
			o.metrics.SessionEvents.WithLabelValues("first_audio_slo_miss").Inc()
		}
	}

	return nil
}

func (o *Orchestrator) profileForSession(s session.Session) PersonaProfile {
	if s.PersonaID != "" {
		if p, ok := o.profiles[s.PersonaID]; ok {
			if p.VoiceID == "" {
				p.VoiceID = o.defaultVoice
			}
			if p.ModelID == "" {
				p.ModelID = o.defaultModel
			}
			if s.VoiceID != "" {
				p.VoiceID = s.VoiceID
			}
			return p
		}
	}
	p := o.profiles["warm"]
	if p.VoiceID == "" {
		p.VoiceID = o.defaultVoice
	}
	if p.ModelID == "" {
		p.ModelID = o.defaultModel
	}
	if s.VoiceID != "" {
		p.VoiceID = s.VoiceID
	}
	return p
}

func (o *Orchestrator) profileForPersona(personaID string) PersonaProfile {
	personaID = strings.TrimSpace(personaID)
	if personaID != "" {
		if p, ok := o.profiles[personaID]; ok {
			if p.VoiceID == "" {
				p.VoiceID = o.defaultVoice
			}
			if p.ModelID == "" {
				p.ModelID = o.defaultModel
			}
			return p
		}
	}

	p := o.profiles["warm"]
	if p.VoiceID == "" {
		p.VoiceID = o.defaultVoice
	}
	if p.ModelID == "" {
		p.ModelID = o.defaultModel
	}
	return p
}

func (o *Orchestrator) send(outbound chan<- any, msg any) {
	select {
	case outbound <- msg:
	default:
		o.metrics.SessionEvents.WithLabelValues("outbound_drop").Inc()
	}
}

func ttsSettingsForProfile(p PersonaProfile) TTSSettings {
	// ElevenLabs voice settings are in [0,1]. Lower stability generally yields more expressive
	// (but less consistent) speech; we tie it loosely to "warmth".
	stability := 0.32 + (1.0-clampFloat(p.Warmth, 0, 1))*0.45
	return TTSSettings{
		Stability:       clampFloat(stability, 0.25, 0.75),
		SimilarityBoost: 0.9,
		Speed:           clampFloat(p.SpeakingRate, 0.7, 1.2),
	}
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

var wakeWordPrefixRe = regexp.MustCompile(`(?i)^\s*(?:(?:hey|hi|ok|okay)\b[\s,.:;!?-]*)?samantha\b[\s,.:;!?-]*\s*(.*)$`)

func detectWakeWordCommitted(text string) (bool, string) {
	// Keep this intentionally conservative: only accept wake word near the start to avoid
	// triggering on unrelated conversation. Return the remainder in the original form.
	m := wakeWordPrefixRe.FindStringSubmatch(text)
	if len(m) == 0 {
		return false, ""
	}
	return true, strings.TrimSpace(m[1])
}
