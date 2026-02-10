package openclaw

import (
	"context"
	"errors"
	"strings"
	"testing"
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
