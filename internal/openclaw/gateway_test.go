package openclaw

import (
	"encoding/json"
	"testing"
)

func TestConnectSignatureStringV2(t *testing.T) {
	got := connectSignatureString(
		"dev1",
		"cli",
		"cli",
		"operator",
		[]string{"operator.read", "operator.write"},
		123,
		"tok",
		"nonce-1",
	)
	want := "v2|dev1|cli|cli|operator|operator.read,operator.write|123|tok|nonce-1"
	if got != want {
		t.Fatalf("connectSignatureString() = %q, want %q", got, want)
	}
}

func TestConnectSignatureStringV1(t *testing.T) {
	got := connectSignatureString(
		"dev1",
		"cli",
		"cli",
		"operator",
		[]string{"a", "b"},
		456,
		"",
		"",
	)
	want := "v1|dev1|cli|cli|operator|a,b|456|"
	if got != want {
		t.Fatalf("connectSignatureString() = %q, want %q", got, want)
	}
}

func TestGatewayAssistantDeltaPrefersDelta(t *testing.T) {
	got := gatewayAssistantDelta(agentAssistantData{
		Text:  "fallback text",
		Delta: "streamed delta",
	})
	if got != "streamed delta" {
		t.Fatalf("gatewayAssistantDelta() = %q, want %q", got, "streamed delta")
	}
}

func TestGatewayAssistantDeltaFallsBackToText(t *testing.T) {
	got := gatewayAssistantDelta(agentAssistantData{
		Text: "streamed text",
	})
	if got != "streamed text" {
		t.Fatalf("gatewayAssistantDelta() = %q, want %q", got, "streamed text")
	}
}

func TestGatewayAssistantDeltaIgnoresWhitespacePayloads(t *testing.T) {
	got := gatewayAssistantDelta(agentAssistantData{
		Text:  "   ",
		Delta: "\n\t",
	})
	if got != "" {
		t.Fatalf("gatewayAssistantDelta() = %q, want empty", got)
	}
}

func TestGatewayIsAcceptedAgentResponse(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "direct status accepted", payload: `{"status":"accepted"}`, want: true},
		{name: "direct accepted bool", payload: `{"accepted":true}`, want: true},
		{name: "nested result accepted", payload: `{"result":{"status":"accepted"}}`, want: true},
		{name: "not accepted", payload: `{"status":"completed"}`, want: false},
		{name: "invalid json", payload: `{`, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gatewayIsAcceptedAgentResponse(json.RawMessage(tc.payload))
			if got != tc.want {
				t.Fatalf("gatewayIsAcceptedAgentResponse(%s) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}

func TestGatewayFinalResponseText(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "payloads array",
			payload: `{"payloads":[{"text":"first"},{"text":"second"}]}`,
			want:    "first\nsecond",
		},
		{
			name:    "nested result data text",
			payload: `{"result":{"data":{"text":"nested"}}}`,
			want:    "nested",
		},
		{
			name:    "top level reply",
			payload: `{"reply":"fallback text"}`,
			want:    "fallback text",
		},
		{
			name:    "empty",
			payload: `{"status":"done"}`,
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gatewayFinalResponseText(json.RawMessage(tc.payload))
			if got != tc.want {
				t.Fatalf("gatewayFinalResponseText(%s) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

func TestResolvedGatewayAgentID(t *testing.T) {
	t.Setenv("OPENCLAW_AGENT_ID", "")
	if got := resolvedGatewayAgentID(); got != "samantha" {
		t.Fatalf("resolvedGatewayAgentID() = %q, want %q", got, "samantha")
	}

	t.Setenv("OPENCLAW_AGENT_ID", "team-agent")
	if got := resolvedGatewayAgentID(); got != "team-agent" {
		t.Fatalf("resolvedGatewayAgentID() = %q, want %q", got, "team-agent")
	}
}

func TestGatewaySessionKey(t *testing.T) {
	got := gatewaySessionKey("  samantha  ", "  session-1  ")
	want := "agent:samantha:session-1"
	if got != want {
		t.Fatalf("gatewaySessionKey() = %q, want %q", got, want)
	}
}

func TestGatewayStreamMayContainAssistantText(t *testing.T) {
	tests := []struct {
		stream string
		want   bool
	}{
		{stream: "assistant", want: true},
		{stream: "assistant.delta", want: true},
		{stream: "response", want: true},
		{stream: "response.output_text", want: true},
		{stream: "output", want: true},
		{stream: "text", want: true},
		{stream: "lifecycle", want: false},
		{stream: "tool", want: false},
		{stream: "", want: false},
	}
	for _, tc := range tests {
		if got := gatewayStreamMayContainAssistantText(tc.stream); got != tc.want {
			t.Fatalf("gatewayStreamMayContainAssistantText(%q) = %v, want %v", tc.stream, got, tc.want)
		}
	}
}

func TestGatewayEventDelta(t *testing.T) {
	tests := []struct {
		name    string
		stream  string
		payload string
		want    string
	}{
		{
			name:    "assistant delta field",
			stream:  "assistant",
			payload: `{"delta":"streamed"}`,
			want:    "streamed",
		},
		{
			name:    "assistant text fallback",
			stream:  "assistant",
			payload: `{"text":"fallback"}`,
			want:    "fallback",
		},
		{
			name:    "response nested text",
			stream:  "response.output_text",
			payload: `{"data":{"text":"nested"}}`,
			want:    "nested",
		},
		{
			name:    "payload array fallback",
			stream:  "response",
			payload: `{"payloads":[{"text":"first"},{"text":"second"}]}`,
			want:    "first\nsecond",
		},
		{
			name:    "ignored stream",
			stream:  "lifecycle",
			payload: `{"text":"should-ignore"}`,
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gatewayEventDelta(tc.stream, json.RawMessage(tc.payload))
			if got != tc.want {
				t.Fatalf("gatewayEventDelta(%q, %s) = %q, want %q", tc.stream, tc.payload, got, tc.want)
			}
		})
	}
}
