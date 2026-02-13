package taskruntime

import (
	"context"
	"testing"
	"time"

	"github.com/ent0n29/samantha/internal/openclaw"
	"github.com/ent0n29/samantha/internal/tasks"
)

func TestServiceCreateRunsTask(t *testing.T) {
	svc := New(Config{
		Enabled:           true,
		TaskTimeout:       5 * time.Second,
		IdempotencyWindow: 10 * time.Second,
	}, openclaw.NewMockAdapter(), nil)

	task, _, err := svc.CreateTask(context.Background(), tasks.CreateRequest{
		SessionID:  "s1",
		UserID:     "u1",
		IntentText: "summarize this plan",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ID == "" {
		t.Fatalf("task.ID empty")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svc.GetTask(task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if got.Status == tasks.TaskStatusCompleted {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task did not complete in time")
}

func TestServiceApprovalFlow(t *testing.T) {
	svc := New(Config{
		Enabled:           true,
		TaskTimeout:       5 * time.Second,
		IdempotencyWindow: 10 * time.Second,
	}, openclaw.NewMockAdapter(), nil)

	task, _, err := svc.CreateTask(context.Background(), tasks.CreateRequest{
		SessionID:  "s2",
		UserID:     "u2",
		IntentText: "deploy a release",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Status != tasks.TaskStatusAwaitingApproval {
		t.Fatalf("task.Status = %q, want %q", task.Status, tasks.TaskStatusAwaitingApproval)
	}

	task, err = svc.ApproveTask(context.Background(), task.ID, true)
	if err != nil {
		t.Fatalf("ApproveTask() error = %v", err)
	}
	if task.Status != tasks.TaskStatusRunning {
		t.Fatalf("task.Status = %q, want %q", task.Status, tasks.TaskStatusRunning)
	}
}

func TestServiceStoreMode(t *testing.T) {
	disabled := New(Config{Enabled: false}, openclaw.NewMockAdapter(), nil)
	if got := disabled.StoreMode(); got != "disabled" {
		t.Fatalf("disabled StoreMode() = %q, want %q", got, "disabled")
	}

	enabled := New(Config{
		Enabled:           true,
		TaskTimeout:       5 * time.Second,
		IdempotencyWindow: 10 * time.Second,
	}, openclaw.NewMockAdapter(), nil)
	if got := enabled.StoreMode(); got != "in-memory" {
		t.Fatalf("enabled StoreMode() = %q, want %q", got, "in-memory")
	}
}
