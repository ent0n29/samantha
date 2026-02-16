package openclaw

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewAdapterAutoFallsBackToMockWhenCLIMissing(t *testing.T) {
	a, err := NewAdapter(Config{
		Mode:    "auto",
		CLIPath: "/definitely/missing/openclaw",
		HTTPURL: "",
	})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	resp, err := a.StreamResponse(context.Background(), MessageRequest{
		InputText: "hello",
	}, nil)
	if err != nil {
		t.Fatalf("StreamResponse() error = %v", err)
	}
	if !strings.Contains(resp.Text, "I heard you: hello") {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
}

func TestFallbackAdapterUsesFallback(t *testing.T) {
	a := NewFallbackAdapter(errAdapter{}, okAdapter{text: "fallback"})
	resp, err := a.StreamResponse(context.Background(), MessageRequest{InputText: "x"}, nil)
	if err != nil {
		t.Fatalf("StreamResponse() error = %v", err)
	}
	if resp.Text != "fallback" {
		t.Fatalf("resp.Text = %q, want fallback", resp.Text)
	}
}

func TestFallbackAdapterSkipsFallbackOnCanceledContext(t *testing.T) {
	fb := &countingAdapter{text: "fallback"}
	a := NewFallbackAdapter(cancelAdapter{}, fb)
	_, err := a.StreamResponse(context.Background(), MessageRequest{InputText: "x"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if fb.calls != 0 {
		t.Fatalf("fallback should not be called, calls = %d", fb.calls)
	}
}

func TestFallbackAdapterFallsBackWhenPrimaryMissesFirstDeltaDeadline(t *testing.T) {
	prev := fallbackFirstDeltaTimeout
	fallbackFirstDeltaTimeout = 40 * time.Millisecond
	t.Cleanup(func() {
		fallbackFirstDeltaTimeout = prev
	})

	fb := &countingAdapter{text: "fallback"}
	a := NewFallbackAdapter(delayedNoDeltaAdapter{delay: 400 * time.Millisecond, text: "primary"}, fb)

	resp, err := a.StreamResponse(context.Background(), MessageRequest{InputText: "x"}, nil)
	if err != nil {
		t.Fatalf("StreamResponse() error = %v", err)
	}
	if resp.Text != "fallback" {
		t.Fatalf("resp.Text = %q, want fallback", resp.Text)
	}
	if fb.calls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fb.calls)
	}
}

func TestFallbackAdapterKeepsPrimaryWhenFirstDeltaArrivesInTime(t *testing.T) {
	prev := fallbackFirstDeltaTimeout
	fallbackFirstDeltaTimeout = 120 * time.Millisecond
	t.Cleanup(func() {
		fallbackFirstDeltaTimeout = prev
	})

	fb := &countingAdapter{text: "fallback"}
	primary := delayedDeltaAdapter{
		firstDelay: 10 * time.Millisecond,
		endDelay:   10 * time.Millisecond,
		text:       "primary",
		delta:      "hello",
	}
	a := NewFallbackAdapter(primary, fb)

	var deltas []string
	resp, err := a.StreamResponse(context.Background(), MessageRequest{InputText: "x"}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamResponse() error = %v", err)
	}
	if resp.Text != "primary" {
		t.Fatalf("resp.Text = %q, want primary", resp.Text)
	}
	if fb.calls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fb.calls)
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatalf("deltas = %q, want %q", strings.Join(deltas, ""), "hello")
	}
}

func TestFallbackAdapterRetriesPrimaryBeforeFallback(t *testing.T) {
	prevTimeout := fallbackFirstDeltaTimeout
	prevRetries := fallbackFirstDeltaRetries
	fallbackFirstDeltaTimeout = 40 * time.Millisecond
	fallbackFirstDeltaRetries = 1
	t.Cleanup(func() {
		fallbackFirstDeltaTimeout = prevTimeout
		fallbackFirstDeltaRetries = prevRetries
	})

	primary := &retryThenDeltaAdapter{
		firstDelay: 220 * time.Millisecond,
		nextDelay:  8 * time.Millisecond,
		text:       "primary",
		delta:      "ready",
	}
	fb := &countingAdapter{text: "fallback"}
	a := NewFallbackAdapter(primary, fb)

	var deltas []string
	resp, err := a.StreamResponse(context.Background(), MessageRequest{InputText: "x"}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamResponse() error = %v", err)
	}
	if resp.Text != "primary" {
		t.Fatalf("resp.Text = %q, want primary", resp.Text)
	}
	if fb.calls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fb.calls)
	}
	if primary.Calls() != 2 {
		t.Fatalf("primary calls = %d, want 2", primary.Calls())
	}
	if strings.Join(deltas, "") != "ready" {
		t.Fatalf("deltas = %q, want %q", strings.Join(deltas, ""), "ready")
	}
}

func TestParseCLIReplyFromRootPayloads(t *testing.T) {
	raw := `{"payloads":[{"text":"hello"},{"text":"world"}],"meta":{"ok":true}}`
	got := parseCLIReply(raw)
	if got != "hello\nworld" {
		t.Fatalf("parseCLIReply() = %q", got)
	}
}

func TestParseCLIReplyFromResultPayloads(t *testing.T) {
	raw := `{
  "runId":"r1",
  "result":{"payloads":[{"text":"line one"},{"text":"line two"}]}
}`
	got := parseCLIReply(raw)
	if got != "line one\nline two" {
		t.Fatalf("parseCLIReply() = %q", got)
	}
}

type errAdapter struct{}

func (errAdapter) StreamResponse(context.Context, MessageRequest, DeltaHandler) (MessageResponse, error) {
	return MessageResponse{}, errors.New("boom")
}

type okAdapter struct {
	text string
}

func (a okAdapter) StreamResponse(context.Context, MessageRequest, DeltaHandler) (MessageResponse, error) {
	return MessageResponse{Text: a.text}, nil
}

type cancelAdapter struct{}

func (cancelAdapter) StreamResponse(context.Context, MessageRequest, DeltaHandler) (MessageResponse, error) {
	return MessageResponse{}, context.Canceled
}

type countingAdapter struct {
	text  string
	calls int
}

func (a *countingAdapter) StreamResponse(context.Context, MessageRequest, DeltaHandler) (MessageResponse, error) {
	a.calls++
	return MessageResponse{Text: a.text}, nil
}

type delayedNoDeltaAdapter struct {
	delay time.Duration
	text  string
}

func (a delayedNoDeltaAdapter) StreamResponse(ctx context.Context, _ MessageRequest, _ DeltaHandler) (MessageResponse, error) {
	timer := time.NewTimer(a.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return MessageResponse{}, ctx.Err()
	case <-timer.C:
		return MessageResponse{Text: a.text}, nil
	}
}

type delayedDeltaAdapter struct {
	firstDelay time.Duration
	endDelay   time.Duration
	text       string
	delta      string
}

type retryThenDeltaAdapter struct {
	mu         sync.Mutex
	calls      int
	firstDelay time.Duration
	nextDelay  time.Duration
	text       string
	delta      string
}

func (a *retryThenDeltaAdapter) Calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *retryThenDeltaAdapter) StreamResponse(ctx context.Context, _ MessageRequest, onDelta DeltaHandler) (MessageResponse, error) {
	a.mu.Lock()
	a.calls++
	call := a.calls
	a.mu.Unlock()

	delay := a.nextDelay
	if call == 1 {
		delay = a.firstDelay
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return MessageResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	if call > 1 && strings.TrimSpace(a.delta) != "" && onDelta != nil {
		if err := onDelta(a.delta); err != nil {
			return MessageResponse{}, err
		}
	}
	return MessageResponse{Text: a.text}, nil
}

func (a delayedDeltaAdapter) StreamResponse(ctx context.Context, _ MessageRequest, onDelta DeltaHandler) (MessageResponse, error) {
	if a.firstDelay > 0 {
		timer := time.NewTimer(a.firstDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return MessageResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	if strings.TrimSpace(a.delta) != "" && onDelta != nil {
		if err := onDelta(a.delta); err != nil {
			return MessageResponse{}, err
		}
	}
	if a.endDelay > 0 {
		timer := time.NewTimer(a.endDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return MessageResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	return MessageResponse{Text: a.text}, nil
}
