package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MessageType identifies websocket payload variants.
type MessageType string

const (
	TypeClientAudioChunk       MessageType = "client_audio_chunk"
	TypeClientControl          MessageType = "client_control"
	TypeSTTPartial             MessageType = "stt_partial"
	TypeSTTCommitted           MessageType = "stt_committed"
	TypeSemanticEndpointHint   MessageType = "semantic_endpoint_hint"
	TypeAssistantThinkingDelta MessageType = "assistant_thinking_delta"
	TypeAssistantTextDelta     MessageType = "assistant_text_delta"
	TypeAssistantAudio         MessageType = "assistant_audio_chunk"
	TypeAssistantTurnEnd       MessageType = "assistant_turn_end"
	TypeSystemEvent            MessageType = "system_event"
	TypeErrorEvent             MessageType = "error_event"
	TypeTaskCreated            MessageType = "task_created"
	TypeTaskPlanGraph          MessageType = "task_plan_graph"
	TypeTaskPlanDelta          MessageType = "task_plan_delta"
	TypeTaskStepStarted        MessageType = "task_step_started"
	TypeTaskStepLog            MessageType = "task_step_log"
	TypeTaskStepCompleted      MessageType = "task_step_completed"
	TypeTaskWaitingApproval    MessageType = "task_waiting_approval"
	TypeTaskCompleted          MessageType = "task_completed"
	TypeTaskFailed             MessageType = "task_failed"
	TypeTaskStatusSnapshot     MessageType = "task_status_snapshot"
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
	TaskID    string      `json:"task_id,omitempty"`
	Approved  *bool       `json:"approved,omitempty"`
	Scope     string      `json:"scope,omitempty"`
	Reason    string      `json:"reason,omitempty"`
	TSMs      int64       `json:"ts_ms,omitempty"`
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

type SemanticEndpointHint struct {
	Type         MessageType `json:"type"`
	SessionID    string      `json:"session_id"`
	Reason       string      `json:"reason"`
	Confidence   float64     `json:"confidence"`
	HoldMS       int64       `json:"hold_ms"`
	ShouldCommit bool        `json:"should_commit"`
	TSMs         int64       `json:"ts_ms"`
}

type AssistantThinkingDelta struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TurnID    string      `json:"turn_id"`
	TextDelta string      `json:"text_delta"`
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

type TaskCreated struct {
	Type             MessageType `json:"type"`
	SessionID        string      `json:"session_id"`
	TaskID           string      `json:"task_id"`
	Summary          string      `json:"summary"`
	Status           string      `json:"status"`
	RiskLevel        string      `json:"risk_level,omitempty"`
	RequiresApproval bool        `json:"requires_approval,omitempty"`
}

type TaskPlanGraphNode struct {
	ID               string `json:"id"`
	Seq              int    `json:"seq"`
	Title            string `json:"title"`
	Kind             string `json:"kind,omitempty"`
	Status           string `json:"status,omitempty"`
	RiskLevel        string `json:"risk_level,omitempty"`
	RequiresApproval bool   `json:"requires_approval,omitempty"`
}

type TaskPlanGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

type TaskPlanGraph struct {
	Type      MessageType         `json:"type"`
	SessionID string              `json:"session_id"`
	TaskID    string              `json:"task_id"`
	Status    string              `json:"status,omitempty"`
	Detail    string              `json:"detail,omitempty"`
	Nodes     []TaskPlanGraphNode `json:"nodes,omitempty"`
	Edges     []TaskPlanGraphEdge `json:"edges,omitempty"`
}

type TaskPlanDelta struct {
	Type           MessageType `json:"type"`
	SessionID      string      `json:"session_id"`
	TaskID         string      `json:"task_id"`
	Status         string      `json:"status"`
	TextDelta      string      `json:"text_delta,omitempty"`
	QueuedPosition int         `json:"queued_position,omitempty"`
}

type TaskStepStarted struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TaskID    string      `json:"task_id"`
	StepID    string      `json:"step_id"`
	StepSeq   int         `json:"step_seq,omitempty"`
	Title     string      `json:"title,omitempty"`
	RiskLevel string      `json:"risk_level,omitempty"`
}

type TaskStepLog struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TaskID    string      `json:"task_id"`
	StepID    string      `json:"step_id"`
	TextDelta string      `json:"text_delta,omitempty"`
}

type TaskStepCompleted struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TaskID    string      `json:"task_id"`
	StepID    string      `json:"step_id"`
	Status    string      `json:"status,omitempty"`
}

type TaskWaitingApproval struct {
	Type             MessageType `json:"type"`
	SessionID        string      `json:"session_id"`
	TaskID           string      `json:"task_id"`
	StepID           string      `json:"step_id"`
	RiskLevel        string      `json:"risk_level,omitempty"`
	Prompt           string      `json:"prompt,omitempty"`
	RequiresApproval bool        `json:"requires_approval,omitempty"`
}

type TaskCompleted struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TaskID    string      `json:"task_id"`
	Status    string      `json:"status,omitempty"`
	Result    string      `json:"result,omitempty"`
}

type TaskFailed struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TaskID    string      `json:"task_id"`
	StepID    string      `json:"step_id,omitempty"`
	Status    string      `json:"status,omitempty"`
	Code      string      `json:"code,omitempty"`
	Detail    string      `json:"detail,omitempty"`
}

type TaskStatusSnapshotItem struct {
	TaskID           string `json:"task_id"`
	Summary          string `json:"summary"`
	Status           string `json:"status"`
	RiskLevel        string `json:"risk_level,omitempty"`
	RequiresApproval bool   `json:"requires_approval,omitempty"`
}

type TaskStatusSnapshot struct {
	Type             MessageType              `json:"type"`
	SessionID        string                   `json:"session_id"`
	Active           []TaskStatusSnapshotItem `json:"active"`
	AwaitingApproval []TaskStatusSnapshotItem `json:"awaiting_approval"`
	Planned          []TaskStatusSnapshotItem `json:"planned"`
}

type clientInbound struct {
	Type        MessageType `json:"type"`
	SessionID   string      `json:"session_id"`
	Seq         int         `json:"seq"`
	PCM16Base64 string      `json:"pcm16_base64"`
	SampleRate  int         `json:"sample_rate"`
	TSMs        int64       `json:"ts_ms"`
	Action      string      `json:"action"`
	TaskID      string      `json:"task_id"`
	Approved    *bool       `json:"approved"`
	Scope       string      `json:"scope"`
	Reason      string      `json:"reason"`
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
			TaskID:    inbound.TaskID,
			Approved:  inbound.Approved,
			Scope:     inbound.Scope,
			Reason:    inbound.Reason,
			TSMs:      inbound.TSMs,
		}, nil
	default:
		return nil, ErrUnsupportedType
	}
}
