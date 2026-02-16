package tasks

import "time"

type TaskStatus string

const (
	TaskStatusPlanned          TaskStatus = "planned"
	TaskStatusPaused           TaskStatus = "paused"
	TaskStatusAwaitingApproval TaskStatus = "awaiting_approval"
	TaskStatusRunning          TaskStatus = "running"
	TaskStatusCompleted        TaskStatus = "completed"
	TaskStatusFailed           TaskStatus = "failed"
	TaskStatusCancelled        TaskStatus = "cancelled"
)

type StepStatus string

const (
	StepStatusPlanned          StepStatus = "planned"
	StepStatusPaused           StepStatus = "paused"
	StepStatusAwaitingApproval StepStatus = "awaiting_approval"
	StepStatusRunning          StepStatus = "running"
	StepStatusCompleted        StepStatus = "completed"
	StepStatusFailed           StepStatus = "failed"
	StepStatusCancelled        StepStatus = "cancelled"
)

type RiskLevel string

const (
	RiskLevelLow     RiskLevel = "low"
	RiskLevelMedium  RiskLevel = "medium"
	RiskLevelHigh    RiskLevel = "high"
	RiskLevelBlocked RiskLevel = "blocked"
)

type Task struct {
	ID               string        `json:"id"`
	SessionID        string        `json:"session_id"`
	UserID           string        `json:"user_id"`
	IntentText       string        `json:"intent_text"`
	Summary          string        `json:"summary"`
	PlanGraph        TaskPlanGraph `json:"plan_graph,omitempty"`
	Mode             string        `json:"mode,omitempty"`
	Priority         string        `json:"priority,omitempty"`
	Status           TaskStatus    `json:"status"`
	RiskLevel        RiskLevel     `json:"risk_level"`
	RequiresApproval bool          `json:"requires_approval"`
	CurrentStepID    string        `json:"current_step_id,omitempty"`
	Steps            []TaskStep    `json:"steps"`
	Result           string        `json:"result,omitempty"`
	Error            string        `json:"error,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	StartedAt        *time.Time    `json:"started_at,omitempty"`
	EndedAt          *time.Time    `json:"ended_at,omitempty"`
}

type TaskPlanGraph struct {
	Version int            `json:"version,omitempty"`
	Nodes   []TaskPlanNode `json:"nodes,omitempty"`
	Edges   []TaskPlanEdge `json:"edges,omitempty"`
}

type TaskPlanNode struct {
	ID               string     `json:"id"`
	Seq              int        `json:"seq"`
	Title            string     `json:"title"`
	Kind             string     `json:"kind,omitempty"`
	Status           StepStatus `json:"status,omitempty"`
	RiskLevel        RiskLevel  `json:"risk_level,omitempty"`
	RequiresApproval bool       `json:"requires_approval,omitempty"`
}

type TaskPlanEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

type TaskStep struct {
	ID               string     `json:"id"`
	TaskID           string     `json:"task_id"`
	Seq              int        `json:"seq"`
	Title            string     `json:"title"`
	Status           StepStatus `json:"status"`
	RiskLevel        RiskLevel  `json:"risk_level"`
	RequiresApproval bool       `json:"requires_approval"`
	ToolName         string     `json:"tool_name,omitempty"`
	InputRedacted    string     `json:"input_redacted,omitempty"`
	OutputRedacted   string     `json:"output_redacted,omitempty"`
	Error            string     `json:"error,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
}

type CreateRequest struct {
	SessionID  string `json:"session_id"`
	UserID     string `json:"user_id"`
	IntentText string `json:"intent_text"`
	Mode       string `json:"mode,omitempty"`
	Priority   string `json:"priority,omitempty"`
}

type EventType string

const (
	EventTaskCreated         EventType = "task_created"
	EventTaskPlanDelta       EventType = "task_plan_delta"
	EventTaskPlanGraph       EventType = "task_plan_graph"
	EventTaskStepStarted     EventType = "task_step_started"
	EventTaskStepLog         EventType = "task_step_log"
	EventTaskStepCompleted   EventType = "task_step_completed"
	EventTaskWaitingApproval EventType = "task_waiting_approval"
	EventTaskCompleted       EventType = "task_completed"
	EventTaskFailed          EventType = "task_failed"
)

type Event struct {
	Type             EventType      `json:"type"`
	SessionID        string         `json:"session_id"`
	TaskID           string         `json:"task_id"`
	StepID           string         `json:"step_id,omitempty"`
	StepSeq          int            `json:"step_seq,omitempty"`
	Title            string         `json:"title,omitempty"`
	Status           TaskStatus     `json:"status,omitempty"`
	RiskLevel        RiskLevel      `json:"risk_level,omitempty"`
	RequiresApproval bool           `json:"requires_approval,omitempty"`
	QueuedPosition   int            `json:"queued_position,omitempty"`
	TextDelta        string         `json:"text_delta,omitempty"`
	Prompt           string         `json:"prompt,omitempty"`
	Result           string         `json:"result,omitempty"`
	Code             string         `json:"code,omitempty"`
	Detail           string         `json:"detail,omitempty"`
	Graph            *TaskPlanGraph `json:"graph,omitempty"`
	At               time.Time      `json:"at"`
}

func (t Task) Clone() Task {
	out := t
	if t.Steps != nil {
		out.Steps = make([]TaskStep, len(t.Steps))
		copy(out.Steps, t.Steps)
	}
	if t.PlanGraph.Nodes != nil {
		out.PlanGraph.Nodes = make([]TaskPlanNode, len(t.PlanGraph.Nodes))
		copy(out.PlanGraph.Nodes, t.PlanGraph.Nodes)
	}
	if t.PlanGraph.Edges != nil {
		out.PlanGraph.Edges = make([]TaskPlanEdge, len(t.PlanGraph.Edges))
		copy(out.PlanGraph.Edges, t.PlanGraph.Edges)
	}
	return out
}

func (t Task) Terminal() bool {
	switch t.Status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}
