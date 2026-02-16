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
