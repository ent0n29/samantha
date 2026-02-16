package tasks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTaskNotFound     = errors.New("task not found")
	ErrInvalidTaskState = errors.New("invalid task state")
)

const defaultEventHistoryLimit = 512

type idempotencyEntry struct {
	TaskID    string
	CreatedAt time.Time
}

type Manager struct {
	mu sync.RWMutex

	idempotencyWindow time.Duration
	store             Store

	tasks            map[string]*Task
	tasksBySession   map[string][]string
	activeBySession  map[string]string
	pendingBySession map[string][]string
	idempotency      map[string]idempotencyEntry
	eventsByTask     map[string][]Event
	eventHistoryMax  int

	subscribers map[string]map[int]chan Event
	nextSubID   int
}

func NewManager(idempotencyWindow time.Duration) *Manager {
	if idempotencyWindow <= 0 {
		idempotencyWindow = 10 * time.Second
	}
	return &Manager{
		idempotencyWindow: idempotencyWindow,
		tasks:             make(map[string]*Task),
		tasksBySession:    make(map[string][]string),
		activeBySession:   make(map[string]string),
		pendingBySession:  make(map[string][]string),
		idempotency:       make(map[string]idempotencyEntry),
		eventsByTask:      make(map[string][]Event),
		eventHistoryMax:   defaultEventHistoryLimit,
		subscribers:       make(map[string]map[int]chan Event),
	}
}

func (m *Manager) SetStore(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
}

func (m *Manager) Subscribe(sessionID string) (<-chan Event, func()) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan Event, 256)
	m.mu.Lock()
	m.nextSubID++
	id := m.nextSubID
	if _, ok := m.subscribers[sessionID]; !ok {
		m.subscribers[sessionID] = make(map[int]chan Event)
	}
	m.subscribers[sessionID][id] = ch
	m.mu.Unlock()

	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		subs := m.subscribers[sessionID]
		if subs == nil {
			return
		}
		if c, ok := subs[id]; ok {
			delete(subs, id)
			close(c)
		}
		if len(subs) == 0 {
			delete(m.subscribers, sessionID)
		}
	}
}

func (m *Manager) Create(req CreateRequest, summary string, risk RiskLevel, requiresApproval bool) (Task, bool, string, error) {
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.IntentText = strings.TrimSpace(req.IntentText)
	req.Mode = strings.TrimSpace(req.Mode)
	req.Priority = strings.TrimSpace(req.Priority)
	summary = strings.TrimSpace(summary)
	if req.SessionID == "" {
		return Task{}, false, "", errors.New("session_id is required")
	}
	if req.IntentText == "" {
		return Task{}, false, "", errors.New("intent_text is required")
	}
	if summary == "" {
		summary = req.IntentText
	}
	now := time.Now().UTC()
	key := m.idempotencyKey(req.SessionID, req.IntentText)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcIdempotencyLocked(now)

	if hit, ok := m.idempotency[key]; ok {
		if now.Sub(hit.CreatedAt) <= m.idempotencyWindow {
			if t, exists := m.tasks[hit.TaskID]; exists {
				return t.Clone(), true, "", nil
			}
		}
	}

	taskID := uuid.NewString()
	stepID := uuid.NewString()
	stepStatus := StepStatusPlanned
	status := TaskStatusPlanned
	if requiresApproval {
		stepStatus = StepStatusAwaitingApproval
		status = TaskStatusAwaitingApproval
	}

	task := &Task{
		ID:               taskID,
		SessionID:        req.SessionID,
		UserID:           req.UserID,
		IntentText:       req.IntentText,
		Summary:          summary,
		PlanGraph:        BuildPlanGraph(summary, req.IntentText, risk, requiresApproval),
		Mode:             req.Mode,
		Priority:         req.Priority,
		Status:           status,
		RiskLevel:        risk,
		RequiresApproval: requiresApproval,
		CurrentStepID:    stepID,
		Steps: []TaskStep{
			{
				ID:               stepID,
				TaskID:           taskID,
				Seq:              1,
				Title:            summary,
				Status:           stepStatus,
				RiskLevel:        risk,
				RequiresApproval: requiresApproval,
				InputRedacted:    req.IntentText,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.tasks[taskID] = task
	m.tasksBySession[req.SessionID] = append(m.tasksBySession[req.SessionID], taskID)
	m.idempotency[key] = idempotencyEntry{
		TaskID:    taskID,
		CreatedAt: now,
	}

	m.publishLocked(task.SessionID, Event{
		Type:             EventTaskCreated,
		SessionID:        task.SessionID,
		TaskID:           task.ID,
		StepID:           stepID,
		StepSeq:          1,
		Title:            summary,
		Status:           task.Status,
		RiskLevel:        risk,
		RequiresApproval: requiresApproval,
		At:               now,
	})
	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskPlanGraph,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		Status:    task.Status,
		Graph:     clonePlanGraphPtr(task.PlanGraph),
		Detail:    fmt.Sprintf("Planned %d step(s).", len(task.PlanGraph.Nodes)),
		At:        now,
	})

	if requiresApproval {
		m.publishLocked(task.SessionID, Event{
			Type:             EventTaskWaitingApproval,
			SessionID:        task.SessionID,
			TaskID:           task.ID,
			StepID:           stepID,
			StepSeq:          1,
			Title:            summary,
			Status:           task.Status,
			RiskLevel:        risk,
			RequiresApproval: true,
			Prompt:           approvalPrompt(task),
			At:               now,
		})
		m.persistTask(task.Clone())
		return task.Clone(), false, "", nil
	}

	startTaskID := m.startOrQueueLocked(task, now)
	return task.Clone(), false, startTaskID, nil
}

func (m *Manager) Approve(taskID string, approved bool) (Task, string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, "", errors.New("task_id is required")
	}
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, "", ErrTaskNotFound
	}
	if task.Status != TaskStatusAwaitingApproval {
		return Task{}, "", fmt.Errorf("%w: approve is only valid in awaiting_approval", ErrInvalidTaskState)
	}

	if !approved {
		task.Status = TaskStatusFailed
		task.Error = "approval denied"
		task.RequiresApproval = false
		task.UpdatedAt = now
		task.EndedAt = &now
		for i := range task.Steps {
			if task.Steps[i].ID == task.CurrentStepID {
				task.Steps[i].Status = StepStatusFailed
				task.Steps[i].RequiresApproval = false
				task.Steps[i].Error = "approval denied"
				task.Steps[i].EndedAt = &now
				break
			}
		}
		m.publishLocked(task.SessionID, Event{
			Type:      EventTaskFailed,
			SessionID: task.SessionID,
			TaskID:    task.ID,
			StepID:    task.CurrentStepID,
			Status:    task.Status,
			Code:      "approval_denied",
			Detail:    "Task approval was denied.",
			At:        now,
		})
		m.persistTask(task.Clone())
		return task.Clone(), "", nil
	}

	task.RequiresApproval = false
	task.Status = TaskStatusPlanned
	task.UpdatedAt = now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			task.Steps[i].Status = StepStatusPlanned
			task.Steps[i].RequiresApproval = false
			break
		}
	}

	startTaskID := m.startOrQueueLocked(task, now)
	return task.Clone(), startTaskID, nil
}

func (m *Manager) AppendStepLog(taskID, delta string) error {
	taskID = strings.TrimSpace(taskID)
	delta = strings.TrimSpace(delta)
	if taskID == "" || delta == "" {
		return nil
	}
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if task.Terminal() {
		return nil
	}
	task.UpdatedAt = now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			if task.Steps[i].OutputRedacted == "" {
				task.Steps[i].OutputRedacted = delta
			} else {
				task.Steps[i].OutputRedacted += "\n" + delta
			}
			break
		}
	}

	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskStepLog,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		StepID:    task.CurrentStepID,
		TextDelta: delta,
		Status:    task.Status,
		At:        now,
	})
	return nil
}

func (m *Manager) Complete(taskID, result string) (Task, string, error) {
	taskID = strings.TrimSpace(taskID)
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, "", ErrTaskNotFound
	}
	if task.Terminal() {
		return task.Clone(), "", nil
	}
	task.Status = TaskStatusCompleted
	task.Result = strings.TrimSpace(result)
	task.Error = ""
	task.UpdatedAt = now
	task.EndedAt = &now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			task.Steps[i].Status = StepStatusCompleted
			task.Steps[i].EndedAt = &now
			break
		}
	}

	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskStepCompleted,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		StepID:    task.CurrentStepID,
		Status:    task.Status,
		At:        now,
	})
	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskCompleted,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		Status:    task.Status,
		Result:    task.Result,
		At:        now,
	})
	m.persistTask(task.Clone())

	nextID := m.releaseAndStartNextLocked(task.SessionID, task.ID, now)
	return task.Clone(), nextID, nil
}

func (m *Manager) Fail(taskID, code, detail string) (Task, string, error) {
	taskID = strings.TrimSpace(taskID)
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, "", ErrTaskNotFound
	}
	if task.Terminal() {
		return task.Clone(), "", nil
	}
	task.Status = TaskStatusFailed
	task.Error = strings.TrimSpace(detail)
	task.UpdatedAt = now
	task.EndedAt = &now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			task.Steps[i].Status = StepStatusFailed
			task.Steps[i].Error = strings.TrimSpace(detail)
			task.Steps[i].EndedAt = &now
			break
		}
	}

	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskFailed,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		StepID:    task.CurrentStepID,
		Status:    task.Status,
		Code:      strings.TrimSpace(code),
		Detail:    strings.TrimSpace(detail),
		At:        now,
	})
	m.persistTask(task.Clone())

	nextID := m.releaseAndStartNextLocked(task.SessionID, task.ID, now)
	return task.Clone(), nextID, nil
}

func (m *Manager) Cancel(taskID, reason string) (Task, string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, "", errors.New("task_id is required")
	}
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, "", ErrTaskNotFound
	}
	if task.Terminal() {
		return task.Clone(), "", nil
	}

	task.Status = TaskStatusCancelled
	task.Error = strings.TrimSpace(reason)
	task.UpdatedAt = now
	task.EndedAt = &now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			task.Steps[i].Status = StepStatusCancelled
			task.Steps[i].Error = strings.TrimSpace(reason)
			task.Steps[i].EndedAt = &now
			break
		}
	}

	m.removePendingLocked(task.SessionID, task.ID)

	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskFailed,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		StepID:    task.CurrentStepID,
		Status:    task.Status,
		Code:      "cancelled",
		Detail:    strings.TrimSpace(reason),
		At:        now,
	})
	m.persistTask(task.Clone())

	nextID := m.releaseAndStartNextLocked(task.SessionID, task.ID, now)
	return task.Clone(), nextID, nil
}

func (m *Manager) Pause(taskID, reason string) (Task, string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, "", errors.New("task_id is required")
	}
	now := time.Now().UTC()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Paused by user."
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, "", ErrTaskNotFound
	}
	if task.Terminal() {
		return task.Clone(), "", nil
	}
	if task.Status == TaskStatusPaused {
		return task.Clone(), "", nil
	}
	wasActive := m.activeBySession[task.SessionID] == task.ID

	task.Status = TaskStatusPaused
	task.UpdatedAt = now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			if task.Steps[i].Status == StepStatusRunning || task.Steps[i].Status == StepStatusPlanned {
				task.Steps[i].Status = StepStatusPaused
			}
			break
		}
	}

	m.removePendingLocked(task.SessionID, task.ID)
	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskPlanDelta,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		Status:    task.Status,
		TextDelta: reason,
		Detail:    reason,
		At:        now,
	})
	m.persistTask(task.Clone())

	nextID := ""
	if wasActive {
		nextID = m.releaseAndStartNextLocked(task.SessionID, task.ID, now)
	}
	return task.Clone(), nextID, nil
}

func (m *Manager) Resume(taskID string) (Task, string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, "", errors.New("task_id is required")
	}
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, "", ErrTaskNotFound
	}
	if task.Terminal() {
		return task.Clone(), "", nil
	}
	if task.Status != TaskStatusPaused && task.Status != TaskStatusPlanned {
		return task.Clone(), "", fmt.Errorf("%w: resume is only valid in paused|planned", ErrInvalidTaskState)
	}

	task.Status = TaskStatusPlanned
	task.UpdatedAt = now
	for i := range task.Steps {
		if task.Steps[i].ID == task.CurrentStepID {
			if task.Steps[i].Status == StepStatusPaused {
				task.Steps[i].Status = StepStatusPlanned
			}
			break
		}
	}

	m.removePendingLocked(task.SessionID, task.ID)
	startID := m.startOrQueueLocked(task, now)
	m.publishLocked(task.SessionID, Event{
		Type:      EventTaskPlanDelta,
		SessionID: task.SessionID,
		TaskID:    task.ID,
		Status:    task.Status,
		TextDelta: "Resumed.",
		Detail:    "Resumed.",
		At:        now,
	})
	m.persistTask(task.Clone())

	return task.Clone(), startID, nil
}

func (m *Manager) Get(taskID string) (Task, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, errors.New("task_id is required")
	}
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	var snapshot Task
	if ok && task != nil {
		snapshot = task.Clone()
	}
	store := m.store
	m.mu.RUnlock()
	if ok {
		return snapshot, nil
	}
	if store == nil {
		return Task{}, ErrTaskNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	persisted, err := store.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, ErrStoreNotFound) {
			return Task{}, ErrTaskNotFound
		}
		return Task{}, err
	}
	m.mu.Lock()
	m.ensureTaskCachedLocked(persisted)
	cached := m.tasks[persisted.ID]
	m.mu.Unlock()
	if cached != nil {
		return cached.Clone(), nil
	}
	return persisted.Clone(), nil
}

func (m *Manager) ListBySession(sessionID string, limit int) []Task {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	m.mu.RLock()
	store := m.store

	ids := m.tasksBySession[sessionID]
	memOut := make([]Task, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		if t, ok := m.tasks[ids[i]]; ok && t != nil {
			memOut = append(memOut, t.Clone())
		}
	}
	m.mu.RUnlock()

	if store == nil {
		if len(memOut) == 0 {
			return nil
		}
		if limit <= 0 || limit > len(memOut) {
			limit = len(memOut)
		}
		return memOut[:limit]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	persisted, err := store.ListTasksBySession(ctx, sessionID, limit)
	if err != nil {
		if len(memOut) == 0 {
			return nil
		}
		if limit <= 0 || limit > len(memOut) {
			limit = len(memOut)
		}
		return memOut[:limit]
	}

	merged := make(map[string]Task, len(persisted)+len(memOut))
	for _, t := range persisted {
		merged[t.ID] = t
	}
	for _, t := range memOut {
		merged[t.ID] = t
	}

	out := make([]Task, 0, len(merged))
	for _, t := range merged {
		if len(t.PlanGraph.Nodes) == 0 {
			t.PlanGraph = BuildPlanGraph(t.Summary, t.IntentText, t.RiskLevel, t.RequiresApproval)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	out = out[:limit]

	m.mu.Lock()
	for _, t := range out {
		m.ensureTaskCachedLocked(t)
	}
	m.mu.Unlock()
	return out
}

func (m *Manager) ListEvents(taskID string, limit int) ([]Event, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	if _, err := m.Get(taskID); err != nil {
		return nil, err
	}

	m.mu.RLock()
	events := m.eventsByTask[taskID]
	if len(events) == 0 {
		m.mu.RUnlock()
		return []Event{}, nil
	}
	start := 0
	if limit > 0 && limit < len(events) {
		start = len(events) - limit
	}
	out := make([]Event, len(events)-start)
	copy(out, events[start:])
	m.mu.RUnlock()
	return out, nil
}

func (m *Manager) LatestAwaitingApproval(sessionID string) (Task, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Task{}, errors.New("session_id is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.tasksBySession[sessionID]
	for i := len(ids) - 1; i >= 0; i-- {
		t := m.tasks[ids[i]]
		if t != nil && t.Status == TaskStatusAwaitingApproval {
			return t.Clone(), nil
		}
	}
	return Task{}, ErrTaskNotFound
}

func (m *Manager) ActiveTask(sessionID string) (Task, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Task{}, errors.New("session_id is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id := m.activeBySession[sessionID]
	if id == "" {
		return Task{}, ErrTaskNotFound
	}
	t := m.tasks[id]
	if t == nil {
		return Task{}, ErrTaskNotFound
	}
	return t.Clone(), nil
}

func (m *Manager) LatestPaused(sessionID string) (Task, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Task{}, errors.New("session_id is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.tasksBySession[sessionID]
	for i := len(ids) - 1; i >= 0; i-- {
		t := m.tasks[ids[i]]
		if t != nil && t.Status == TaskStatusPaused {
			return t.Clone(), nil
		}
	}
	return Task{}, ErrTaskNotFound
}

func (m *Manager) idempotencyKey(sessionID, intent string) string {
	normalized := normalizeIntent(intent)
	return sessionID + "|" + normalized
}

func (m *Manager) gcIdempotencyLocked(now time.Time) {
	for k, v := range m.idempotency {
		if now.Sub(v.CreatedAt) > m.idempotencyWindow {
			delete(m.idempotency, k)
		}
	}
}

func (m *Manager) startOrQueueLocked(task *Task, now time.Time) string {
	sessionID := task.SessionID
	if m.activeBySession[sessionID] == "" {
		m.activeBySession[sessionID] = task.ID
		task.Status = TaskStatusRunning
		task.UpdatedAt = now
		if task.StartedAt == nil {
			task.StartedAt = &now
		}
		for i := range task.Steps {
			if task.Steps[i].ID == task.CurrentStepID {
				task.Steps[i].Status = StepStatusRunning
				task.Steps[i].StartedAt = &now
				break
			}
		}
		m.publishLocked(task.SessionID, Event{
			Type:      EventTaskStepStarted,
			SessionID: task.SessionID,
			TaskID:    task.ID,
			StepID:    task.CurrentStepID,
			StepSeq:   1,
			Title:     task.Summary,
			Status:    task.Status,
			RiskLevel: task.RiskLevel,
			At:        now,
		})
		m.persistTask(task.Clone())
		return task.ID
	}

	m.pendingBySession[sessionID] = append(m.pendingBySession[sessionID], task.ID)
	task.Status = TaskStatusPlanned
	task.UpdatedAt = now
	queuedPos := len(m.pendingBySession[sessionID])
	m.publishLocked(task.SessionID, Event{
		Type:           EventTaskPlanDelta,
		SessionID:      task.SessionID,
		TaskID:         task.ID,
		Status:         task.Status,
		QueuedPosition: queuedPos,
		TextDelta:      fmt.Sprintf("Queued (position %d).", queuedPos),
		At:             now,
	})
	return ""
}

func (m *Manager) ensureTaskCachedLocked(task Task) {
	if len(task.PlanGraph.Nodes) == 0 {
		task.PlanGraph = BuildPlanGraph(task.Summary, task.IntentText, task.RiskLevel, task.RequiresApproval)
	}
	cloned := task.Clone()
	t := cloned
	m.tasks[task.ID] = &t

	ids := m.tasksBySession[task.SessionID]
	for _, id := range ids {
		if id == task.ID {
			return
		}
	}
	m.tasksBySession[task.SessionID] = append(m.tasksBySession[task.SessionID], task.ID)
}

func (m *Manager) persistTask(task Task) {
	store := m.store
	if store == nil {
		return
	}

	go func(snapshot Task) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = store.SaveTask(ctx, snapshot)
	}(task.Clone())
}

func (m *Manager) releaseAndStartNextLocked(sessionID, completedTaskID string, now time.Time) string {
	if m.activeBySession[sessionID] == completedTaskID {
		m.activeBySession[sessionID] = ""
	}
	queue := m.pendingBySession[sessionID]
	for len(queue) > 0 {
		nextID := queue[0]
		queue = queue[1:]
		if len(queue) == 0 {
			delete(m.pendingBySession, sessionID)
		} else {
			m.pendingBySession[sessionID] = append([]string(nil), queue...)
		}

		nextTask, ok := m.tasks[nextID]
		if !ok || nextTask == nil || nextTask.Terminal() {
			continue
		}
		return m.startOrQueueLocked(nextTask, now)
	}
	return ""
}

func (m *Manager) removePendingLocked(sessionID, taskID string) {
	queue := m.pendingBySession[sessionID]
	if len(queue) == 0 {
		return
	}
	out := queue[:0]
	for _, id := range queue {
		if id == taskID {
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		delete(m.pendingBySession, sessionID)
		return
	}
	m.pendingBySession[sessionID] = append([]string(nil), out...)
}

func (m *Manager) publishLocked(sessionID string, evt Event) {
	if taskID := strings.TrimSpace(evt.TaskID); taskID != "" {
		m.eventsByTask[taskID] = append(m.eventsByTask[taskID], evt)
		if max := m.eventHistoryMax; max > 0 && len(m.eventsByTask[taskID]) > max {
			trimFrom := len(m.eventsByTask[taskID]) - max
			m.eventsByTask[taskID] = append([]Event(nil), m.eventsByTask[taskID][trimFrom:]...)
		}
	}

	subs := m.subscribers[sessionID]
	if len(subs) == 0 {
		return
	}
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
		}
	}
}

func approvalPrompt(task *Task) string {
	return fmt.Sprintf("This task may mutate your system. Approve task \"%s\"?", task.Summary)
}

func normalizeIntent(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return ""
	}
	parts := strings.Fields(in)
	return strings.Join(parts, " ")
}

func clonePlanGraphPtr(g TaskPlanGraph) *TaskPlanGraph {
	cp := g
	if g.Nodes != nil {
		cp.Nodes = make([]TaskPlanNode, len(g.Nodes))
		copy(cp.Nodes, g.Nodes)
	}
	if g.Edges != nil {
		cp.Edges = make([]TaskPlanEdge, len(g.Edges))
		copy(cp.Edges, g.Edges)
	}
	return &cp
}
