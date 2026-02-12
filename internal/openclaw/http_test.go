package openclaw

import (
	"strings"
	"testing"
)

func TestHTTPAdapterConsumeSSE(t *testing.T) {
	a := NewHTTPAdapterWithOptions("http://example.test", false)
	stream := strings.NewReader(strings.Join([]string{
		": keepalive",
		"",
		"data: {\"delta\":\"Hel\"}",
		"",
		"data: {\"delta\":\"lo\"}",
		"",
		"data: [DONE]",
		"",
	}, "\n"))

	var deltas []string
	resp, err := a.consumeSSE(stream, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("consumeSSE() error = %v", err)
	}
	if resp.Text != "Hello" {
		t.Fatalf("resp.Text = %q, want %q", resp.Text, "Hello")
	}
	if strings.Join(deltas, "") != "Hello" {
		t.Fatalf("deltas = %q, want %q", strings.Join(deltas, ""), "Hello")
	}
}

func TestHTTPAdapterConsumeSSEStrictInvalidJSON(t *testing.T) {
	a := NewHTTPAdapterWithOptions("http://example.test", true)
	stream := strings.NewReader("data: {not-json}\n\n")
	_, err := a.consumeSSE(stream, nil)
	if err == nil {
		t.Fatalf("consumeSSE() expected error for invalid strict payload")
	}
}

func TestHTTPAdapterConsumeNDJSON(t *testing.T) {
	a := NewHTTPAdapterWithOptions("http://example.test", false)
	stream := strings.NewReader(strings.Join([]string{
		"{\"delta\":\"Hi\"}",
		" there",
		"[DONE]",
	}, "\n"))

	resp, err := a.consumeNDJSON(stream, nil)
	if err != nil {
		t.Fatalf("consumeNDJSON() error = %v", err)
	}
	if resp.Text != "Hi there" {
		t.Fatalf("resp.Text = %q, want %q", resp.Text, "Hi there")
	}
}

func TestHTTPAdapterConsumeNDJSONStrictInvalidJSON(t *testing.T) {
	a := NewHTTPAdapterWithOptions("http://example.test", true)
	stream := strings.NewReader("not-json\n")
	_, err := a.consumeNDJSON(stream, nil)
	if err == nil {
		t.Fatalf("consumeNDJSON() expected error for strict invalid payload")
	}
}
