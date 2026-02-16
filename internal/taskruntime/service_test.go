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

func TestServicePauseResumeFlow(t *testing.T) {
	svc := New(Config{
		Enabled:           true,
		TaskTimeout:       10 * time.Second,
		IdempotencyWindow: 10 * time.Second,
	}, blockingAdapter{}, nil)

	task, _, err := svc.CreateTask(context.Background(), tasks.CreateRequest{
		SessionID:  "s3",
		UserID:     "u3",
		IntentText: "capture notes for this session",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	waitTaskStatus(t, svc, task.ID, tasks.TaskStatusRunning, 2*time.Second)

	paused, err := svc.PauseTask(context.Background(), task.ID, "Paused from test.")
	if err != nil {
		t.Fatalf("PauseTask() error = %v", err)
	}
	if paused.Status != tasks.TaskStatusPaused {
		t.Fatalf("paused.Status = %q, want %q", paused.Status, tasks.TaskStatusPaused)
	}

	waitTaskStatus(t, svc, task.ID, tasks.TaskStatusPaused, 2*time.Second)

	resumed, err := svc.ResumeTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if resumed.Status != tasks.TaskStatusRunning {
		t.Fatalf("resumed.Status = %q, want %q", resumed.Status, tasks.TaskStatusRunning)
	}

	waitTaskStatus(t, svc, task.ID, tasks.TaskStatusRunning, 2*time.Second)

	cancelled, err := svc.CancelTask(context.Background(), task.ID, "Cleanup cancel.")
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != tasks.TaskStatusCancelled {
		t.Fatalf("cancelled.Status = %q, want %q", cancelled.Status, tasks.TaskStatusCancelled)
	}
}

type blockingAdapter struct{}

func (blockingAdapter) StreamResponse(ctx context.Context, _ openclaw.MessageRequest, onDelta openclaw.DeltaHandler) (openclaw.MessageResponse, error) {
	if onDelta != nil {
		_ = onDelta("running")
	}
	<-ctx.Done()
	return openclaw.MessageResponse{}, ctx.Err()
}

func waitTaskStatus(t *testing.T, svc *Service, taskID string, want tasks.TaskStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got, err := svc.GetTask(taskID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if got.Status == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	got, err := svc.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask() final error = %v", err)
	}
	t.Fatalf("task status = %q, want %q", got.Status, want)
}
