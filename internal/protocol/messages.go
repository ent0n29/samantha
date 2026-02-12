package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MessageType identifies websocket payload variants.
type MessageType string

const (
	TypeClientAudioChunk   MessageType = "client_audio_chunk"
	TypeClientControl      MessageType = "client_control"
	TypeSTTPartial         MessageType = "stt_partial"
	TypeSTTCommitted       MessageType = "stt_committed"
	TypeAssistantTextDelta MessageType = "assistant_text_delta"
	TypeAssistantAudio     MessageType = "assistant_audio_chunk"
	TypeAssistantTurnEnd   MessageType = "assistant_turn_end"
	TypeSystemEvent        MessageType = "system_event"
	TypeErrorEvent         MessageType = "error_event"
)

var ErrUnsupportedType = errors.New("unsupported message type")

type Envelope struct {
	Type MessageType `json:"type"`
}

type ClientAudioChunk struct {
	Type        MessageType `json:"type"`
	SessionID   string      `json:"session_id"`
	Seq         int         `json:"seq"`
	PCM16Base64 string      `json:"pcm16_base64"`
	SampleRate  int         `json:"sample_rate"`
	TSMs        int64       `json:"ts_ms"`
}

type ClientControl struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	Action    string      `json:"action"`
}

type STTPartial struct {
	Type       MessageType `json:"type"`
	SessionID  string      `json:"session_id"`
	Text       string      `json:"text"`
	Confidence float64     `json:"confidence"`
	TSMs       int64       `json:"ts_ms"`
}

type STTCommitted struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	Text      string      `json:"text"`
	TSMs      int64       `json:"ts_ms"`
}

type AssistantTextDelta struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TurnID    string      `json:"turn_id"`
	TextDelta string      `json:"text_delta"`
}

type AssistantAudioChunk struct {
	Type        MessageType `json:"type"`
	SessionID   string      `json:"session_id"`
	TurnID      string      `json:"turn_id"`
	Seq         int         `json:"seq"`
	Format      string      `json:"format"`
	AudioBase64 string      `json:"audio_base64"`
}

type AssistantTurnEnd struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TurnID    string      `json:"turn_id"`
	Reason    string      `json:"reason"`
}

type SystemEvent struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	Code      string      `json:"code"`
	Detail    string      `json:"detail,omitempty"`
}

type ErrorEvent struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	Code      string      `json:"code"`
	Source    string      `json:"source"`
	Retryable bool        `json:"retryable"`
	Detail    string      `json:"detail"`
}

type clientInbound struct {
	Type        MessageType `json:"type"`
	SessionID   string      `json:"session_id"`
	Seq         int         `json:"seq"`
	PCM16Base64 string      `json:"pcm16_base64"`
	SampleRate  int         `json:"sample_rate"`
	TSMs        int64       `json:"ts_ms"`
	Action      string      `json:"action"`
}

func ParseClientMessage(raw []byte) (any, error) {
	var inbound clientInbound
	if err := json.Unmarshal(raw, &inbound); err != nil {
		return nil, fmt.Errorf("invalid envelope: %w", err)
	}

	switch inbound.Type {
	case TypeClientAudioChunk:
		if inbound.SessionID == "" || inbound.PCM16Base64 == "" || inbound.SampleRate <= 0 {
			return nil, errors.New("invalid client_audio_chunk")
		}
		return ClientAudioChunk{
			Type:        TypeClientAudioChunk,
			SessionID:   inbound.SessionID,
			Seq:         inbound.Seq,
			PCM16Base64: inbound.PCM16Base64,
			SampleRate:  inbound.SampleRate,
			TSMs:        inbound.TSMs,
		}, nil
	case TypeClientControl:
		if inbound.SessionID == "" || inbound.Action == "" {
			return nil, errors.New("invalid client_control")
		}
		return ClientControl{
			Type:      TypeClientControl,
			SessionID: inbound.SessionID,
			Action:    inbound.Action,
		}, nil
	default:
		return nil, ErrUnsupportedType
	}
}
