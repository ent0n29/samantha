package voice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ent0n29/samantha/internal/openclaw"
)

type retryStep struct {
	beforeDelta time.Duration
	delta       string
	finalText   string
	err         error
}

type retryTestAdapter struct {
	mu    sync.Mutex
	steps []retryStep
	calls int
}

func (a *retryTestAdapter) Calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *retryTestAdapter) nextStep() retryStep {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if len(a.steps) == 0 {
		return retryStep{}
	}
	step := a.steps[0]
	if len(a.steps) > 1 {
		a.steps = a.steps[1:]
	}
	return step
}

func (a *retryTestAdapter) StreamResponse(ctx context.Context, _ openclaw.MessageRequest, onDelta openclaw.DeltaHandler) (openclaw.MessageResponse, error) {
	step := a.nextStep()
	if step.beforeDelta > 0 {
		timer := time.NewTimer(step.beforeDelta)
		select {
		case <-ctx.Done():
			timer.Stop()
			return openclaw.MessageResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	if strings.TrimSpace(step.delta) != "" && onDelta != nil {
		if err := onDelta(step.delta); err != nil {
			return openclaw.MessageResponse{}, err
		}
	}
	if step.err != nil {
		return openclaw.MessageResponse{}, step.err
	}
	return openclaw.MessageResponse{Text: step.finalText}, nil
}

func TestStreamResponseWithFirstDeltaRetryRetriesOnTimeoutWithoutDelta(t *testing.T) {
	adapter := &retryTestAdapter{
		steps: []retryStep{
			{beforeDelta: 220 * time.Millisecond, finalText: "late"},
			{beforeDelta: 5 * time.Millisecond, delta: "fast", finalText: "fast"},
		},
	}
	var deltas []string
	resp, retries, err := streamResponseWithFirstDeltaRetry(
		context.Background(),
		adapter,
		openclaw.MessageRequest{InputText: "hello"},
		func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
		40*time.Millisecond,
		1,
	)
	if err != nil {
		t.Fatalf("streamResponseWithFirstDeltaRetry() error = %v", err)
	}
	if retries != 1 {
		t.Fatalf("retries = %d, want 1", retries)
	}
	if adapter.Calls() != 2 {
		t.Fatalf("adapter calls = %d, want 2", adapter.Calls())
	}
	if resp.Text != "fast" {
		t.Fatalf("resp.Text = %q, want %q", resp.Text, "fast")
	}
	if strings.Join(deltas, "") != "fast" {
		t.Fatalf("deltas = %q, want %q", strings.Join(deltas, ""), "fast")
	}
}

func TestStreamResponseWithFirstDeltaRetryDoesNotRetryAfterFirstDelta(t *testing.T) {
	adapter := &retryTestAdapter{
		steps: []retryStep{
			{beforeDelta: 5 * time.Millisecond, delta: "hello", finalText: "hello"},
		},
	}
	resp, retries, err := streamResponseWithFirstDeltaRetry(
		context.Background(),
		adapter,
		openclaw.MessageRequest{InputText: "hello"},
		nil,
		60*time.Millisecond,
		1,
	)
	if err != nil {
		t.Fatalf("streamResponseWithFirstDeltaRetry() error = %v", err)
	}
	if retries != 0 {
		t.Fatalf("retries = %d, want 0", retries)
	}
	if adapter.Calls() != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.Calls())
	}
	if resp.Text != "hello" {
		t.Fatalf("resp.Text = %q, want %q", resp.Text, "hello")
	}
}

func TestStreamResponseWithFirstDeltaRetryStopsOnNonTimeoutError(t *testing.T) {
	adapter := &retryTestAdapter{
		steps: []retryStep{
			{beforeDelta: 5 * time.Millisecond, err: errors.New("boom")},
		},
	}
	_, retries, err := streamResponseWithFirstDeltaRetry(
		context.Background(),
		adapter,
		openclaw.MessageRequest{InputText: "hello"},
		nil,
		60*time.Millisecond,
		1,
	)
	if err == nil {
		t.Fatalf("expected error")
	}
	if retries != 0 {
		t.Fatalf("retries = %d, want 0", retries)
	}
	if adapter.Calls() != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.Calls())
	}
}

func TestStreamResponseWithFirstDeltaRetryReturnsTimeoutErrorAfterExhaustion(t *testing.T) {
	adapter := &retryTestAdapter{
		steps: []retryStep{
			{beforeDelta: 220 * time.Millisecond, finalText: "late-1"},
			{beforeDelta: 220 * time.Millisecond, finalText: "late-2"},
		},
	}
	_, retries, err := streamResponseWithFirstDeltaRetry(
		context.Background(),
		adapter,
		openclaw.MessageRequest{InputText: "hello"},
		nil,
		40*time.Millisecond,
		1,
	)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, errBrainFirstDeltaTimeout) {
		t.Fatalf("error = %v, want errBrainFirstDeltaTimeout", err)
	}
	if retries != 1 {
		t.Fatalf("retries = %d, want 1", retries)
	}
	if adapter.Calls() != 2 {
		t.Fatalf("adapter calls = %d, want 2", adapter.Calls())
	}
}

func TestDisableBrainFirstDeltaRetry(t *testing.T) {
	if disableBrainFirstDeltaRetry(nil) {
		t.Fatalf("disableBrainFirstDeltaRetry(nil) = true, want false")
	}
	if !disableBrainFirstDeltaRetry(&openclaw.GatewayAdapter{}) {
		t.Fatalf("disableBrainFirstDeltaRetry(gateway) = false, want true")
	}
	if !disableBrainFirstDeltaRetry(openclaw.NewFallbackAdapter(&retryTestAdapter{}, &retryTestAdapter{})) {
		t.Fatalf("disableBrainFirstDeltaRetry(fallback) = false, want true")
	}
	if disableBrainFirstDeltaRetry(&retryTestAdapter{}) {
		t.Fatalf("disableBrainFirstDeltaRetry(retryTestAdapter) = true, want false")
	}
}
