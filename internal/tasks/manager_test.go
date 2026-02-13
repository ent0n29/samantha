package tasks

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestManagerCreateDedup(t *testing.T) {
	m := NewManager(10 * time.Second)
	req := CreateRequest{
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "build a landing page",
	}

	first, dedup, startID, err := m.Create(req, "build a landing page", RiskLevelMedium, false)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if dedup {
		t.Fatalf("first create dedup = true, want false")
	}
	if startID == "" {
		t.Fatalf("first create startID empty, want task id")
	}

	second, dedup, startID, err := m.Create(req, "build a landing page", RiskLevelMedium, false)
	if err != nil {
		t.Fatalf("Create() second error = %v", err)
	}
	if !dedup {
		t.Fatalf("second create dedup = false, want true")
	}
	if startID != "" {
		t.Fatalf("second create startID = %q, want empty", startID)
	}
	if second.ID != first.ID {
		t.Fatalf("second task id = %q, want %q", second.ID, first.ID)
	}
}

func TestManagerApprovalFlow(t *testing.T) {
	m := NewManager(10 * time.Second)
	task, _, startID, err := m.Create(CreateRequest{
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "deploy the app",
	}, "deploy the app", RiskLevelHigh, true)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if startID != "" {
		t.Fatalf("startID = %q, want empty for awaiting approval", startID)
	}
	if task.Status != TaskStatusAwaitingApproval {
		t.Fatalf("task.Status = %q, want %q", task.Status, TaskStatusAwaitingApproval)
	}

	approved, startID, err := m.Approve(task.ID, true)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved.Status != TaskStatusRunning {
		t.Fatalf("approved.Status = %q, want %q", approved.Status, TaskStatusRunning)
	}
	if startID == "" {
		t.Fatalf("startID empty after approve")
	}
}

func TestManagerQueueNextAfterComplete(t *testing.T) {
	m := NewManager(10 * time.Second)
	a, _, startA, err := m.Create(CreateRequest{
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "task a",
	}, "task a", RiskLevelLow, false)
	if err != nil {
		t.Fatalf("Create(a) error = %v", err)
	}
	if startA == "" {
		t.Fatalf("startA empty")
	}

	b, _, startB, err := m.Create(CreateRequest{
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "task b",
	}, "task b", RiskLevelLow, false)
	if err != nil {
		t.Fatalf("Create(b) error = %v", err)
	}
	if startB != "" {
		t.Fatalf("startB = %q, want empty because queued", startB)
	}
	if b.Status != TaskStatusPlanned {
		t.Fatalf("b.Status = %q, want %q", b.Status, TaskStatusPlanned)
	}

	_, nextID, err := m.Complete(a.ID, "done")
	if err != nil {
		t.Fatalf("Complete(a) error = %v", err)
	}
	if nextID != b.ID {
		t.Fatalf("nextID = %q, want %q", nextID, b.ID)
	}
}

func TestManagerGetFallsBackToStoreAndCaches(t *testing.T) {
	now := time.Now().UTC()
	persisted := Task{
		ID:         "task-store-1",
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "from store",
		Summary:    "from store",
		Status:     TaskStatusCompleted,
		RiskLevel:  RiskLevelLow,
		CreatedAt:  now,
		UpdatedAt:  now,
		Steps: []TaskStep{
			{
				ID:        "step-store-1",
				TaskID:    "task-store-1",
				Seq:       1,
				Title:     "from store",
				Status:    StepStatusCompleted,
				RiskLevel: RiskLevelLow,
			},
		},
	}

	store := newFakeTaskStore([]Task{persisted})
	m := NewManager(10 * time.Second)
	m.SetStore(store)

	got, err := m.Get(persisted.ID)
	if err != nil {
		t.Fatalf("Get() from store error = %v", err)
	}
	if got.ID != persisted.ID {
		t.Fatalf("Get() id = %q, want %q", got.ID, persisted.ID)
	}

	store.delete(persisted.ID)
	gotCached, err := m.Get(persisted.ID)
	if err != nil {
		t.Fatalf("Get() from cache error = %v", err)
	}
	if gotCached.ID != persisted.ID {
		t.Fatalf("cached id = %q, want %q", gotCached.ID, persisted.ID)
	}
}

func TestManagerListBySessionMergesStoreAndMemory(t *testing.T) {
	now := time.Now().UTC()
	persisted := Task{
		ID:         "task-store-2",
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "older task",
		Summary:    "older task",
		Status:     TaskStatusCompleted,
		RiskLevel:  RiskLevelLow,
		CreatedAt:  now.Add(-2 * time.Minute),
		UpdatedAt:  now.Add(-2 * time.Minute),
		Steps: []TaskStep{
			{
				ID:        "step-store-2",
				TaskID:    "task-store-2",
				Seq:       1,
				Title:     "older task",
				Status:    StepStatusCompleted,
				RiskLevel: RiskLevelLow,
			},
		},
	}

	store := newFakeTaskStore([]Task{persisted})
	m := NewManager(10 * time.Second)
	m.SetStore(store)

	inMem, _, _, err := m.Create(CreateRequest{
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "newer task",
	}, "newer task", RiskLevelLow, false)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	list := m.ListBySession("s1", 10)
	if len(list) < 2 {
		t.Fatalf("ListBySession len = %d, want at least 2", len(list))
	}

	seen := map[string]bool{}
	for _, task := range list {
		seen[task.ID] = true
	}
	if !seen[persisted.ID] {
		t.Fatalf("persisted task %q missing from list", persisted.ID)
	}
	if !seen[inMem.ID] {
		t.Fatalf("in-memory task %q missing from list", inMem.ID)
	}

	if !list[0].CreatedAt.After(list[1].CreatedAt) && !list[0].CreatedAt.Equal(list[1].CreatedAt) {
		t.Fatalf("tasks not sorted by created_at desc")
	}

	limited := m.ListBySession("s1", 1)
	if len(limited) != 1 {
		t.Fatalf("ListBySession(limit=1) len = %d, want 1", len(limited))
	}
}

func TestManagerListEventsRespectsLimit(t *testing.T) {
	m := NewManager(10 * time.Second)
	task, _, _, err := m.Create(CreateRequest{
		SessionID:  "s-events",
		UserID:     "u-events",
		IntentText: "note task events",
	}, "note task events", RiskLevelLow, false)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := m.AppendStepLog(task.ID, "first line"); err != nil {
		t.Fatalf("AppendStepLog(first) error = %v", err)
	}
	if err := m.AppendStepLog(task.ID, "second line"); err != nil {
		t.Fatalf("AppendStepLog(second) error = %v", err)
	}
	if _, _, err := m.Complete(task.ID, "done"); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	events, err := m.ListEvents(task.ID, 3)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEvents() len = %d, want 3", len(events))
	}
	if events[0].Type != EventTaskStepLog {
		t.Fatalf("events[0].Type = %q, want %q", events[0].Type, EventTaskStepLog)
	}
	if events[1].Type != EventTaskStepCompleted {
		t.Fatalf("events[1].Type = %q, want %q", events[1].Type, EventTaskStepCompleted)
	}
	if events[2].Type != EventTaskCompleted {
		t.Fatalf("events[2].Type = %q, want %q", events[2].Type, EventTaskCompleted)
	}
}

type fakeTaskStore struct {
	mu    sync.Mutex
	tasks map[string]Task
}

func newFakeTaskStore(seed []Task) *fakeTaskStore {
	out := &fakeTaskStore{
		tasks: make(map[string]Task, len(seed)),
	}
	for _, task := range seed {
		out.tasks[task.ID] = task.Clone()
	}
	return out
}

func (s *fakeTaskStore) SaveTask(_ context.Context, task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task.Clone()
	return nil
}

func (s *fakeTaskStore) GetTask(_ context.Context, taskID string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return Task{}, ErrStoreNotFound
	}
	return task.Clone(), nil
}

func (s *fakeTaskStore) ListTasksBySession(_ context.Context, sessionID string, limit int) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if task.SessionID == sessionID {
			out = append(out, task.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (s *fakeTaskStore) Close() error {
	return nil
}

func (s *fakeTaskStore) delete(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, taskID)
}
