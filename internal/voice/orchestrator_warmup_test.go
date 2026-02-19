package voice

import (
	"context"
	"testing"
	"time"

	"github.com/ent0n29/samantha/internal/openclaw"
)

type warmupProbeAdapter struct {
	calls chan string
}

func (a *warmupProbeAdapter) StreamResponse(context.Context, openclaw.MessageRequest, openclaw.DeltaHandler) (openclaw.MessageResponse, error) {
	return openclaw.MessageResponse{}, nil
}

func (a *warmupProbeAdapter) PrewarmSession(_ context.Context, sessionID string) error {
	if a.calls != nil {
		a.calls <- sessionID
	}
	return nil
}

type warmupPlainAdapter struct{}

func (a *warmupPlainAdapter) StreamResponse(context.Context, openclaw.MessageRequest, openclaw.DeltaHandler) (openclaw.MessageResponse, error) {
	return openclaw.MessageResponse{}, nil
}

func TestPrewarmAdapterPrefersDirectCapableAdapter(t *testing.T) {
	probe := &warmupProbeAdapter{calls: make(chan string, 1)}
	o := &Orchestrator{adapter: probe}

	got := o.prewarmAdapter()
	if got == nil {
		t.Fatalf("prewarmAdapter() returned nil")
	}
	typed, ok := got.(*warmupProbeAdapter)
	if !ok {
		t.Fatalf("prewarmAdapter() type = %T, want *warmupProbeAdapter", got)
	}
	if typed != probe {
		t.Fatalf("prewarmAdapter() returned unexpected adapter instance")
	}
}

func TestPrewarmAdapterUsesFallbackPrimary(t *testing.T) {
	primary := &warmupProbeAdapter{calls: make(chan string, 1)}
	fallback := &warmupPlainAdapter{}
	o := &Orchestrator{
		adapter: openclaw.NewFallbackAdapter(primary, fallback),
	}

	got := o.prewarmAdapter()
	if got == nil {
		t.Fatalf("prewarmAdapter() returned nil")
	}
	typed, ok := got.(*warmupProbeAdapter)
	if !ok {
		t.Fatalf("prewarmAdapter() type = %T, want *warmupProbeAdapter", got)
	}
	if typed != primary {
		t.Fatalf("prewarmAdapter() did not return fallback primary adapter")
	}
}

func TestPrewarmAdapterReturnsNilWhenUnavailable(t *testing.T) {
	var nilOrchestrator *Orchestrator
	if got := nilOrchestrator.prewarmAdapter(); got != nil {
		t.Fatalf("nil orchestrator prewarmAdapter() = %T, want nil", got)
	}

	o := &Orchestrator{adapter: &warmupPlainAdapter{}}
	if got := o.prewarmAdapter(); got != nil {
		t.Fatalf("prewarmAdapter() = %T, want nil", got)
	}
}

func TestStartBrainSessionWarmupCallsAdapter(t *testing.T) {
	probe := &warmupProbeAdapter{calls: make(chan string, 1)}
	o := &Orchestrator{adapter: probe}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	o.startBrainSessionWarmup(ctx, "  session-42  ")

	select {
	case got := <-probe.calls:
		if got != "session-42" {
			t.Fatalf("warmup session id = %q, want %q", got, "session-42")
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatalf("timed out waiting for PrewarmSession call")
	}
}

func TestStartBrainSessionWarmupSkipsBlankSessionID(t *testing.T) {
	probe := &warmupProbeAdapter{calls: make(chan string, 1)}
	o := &Orchestrator{adapter: probe}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	o.startBrainSessionWarmup(ctx, "   ")

	select {
	case got := <-probe.calls:
		t.Fatalf("unexpected PrewarmSession call with session %q", got)
	case <-time.After(120 * time.Millisecond):
	}
}
