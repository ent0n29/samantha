package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antoniostano/samantha/internal/config"
	"github.com/antoniostano/samantha/internal/observability"
	"github.com/antoniostano/samantha/internal/session"
)

func TestCreateAndEndSession(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	createReq := map[string]string{
		"user_id":    "user-1",
		"persona_id": "warm",
	}
	body, _ := json.Marshal(createReq)
	res, err := http.Post(ts.URL+"/v1/voice/session", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session request error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", res.StatusCode, http.StatusCreated)
	}

	var created map[string]any
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("missing session_id in create response: %+v", created)
	}

	endRes, err := http.Post(ts.URL+"/v1/voice/session/"+sessionID+"/end", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("end session request error = %v", err)
	}
	defer endRes.Body.Close()
	if endRes.StatusCode != http.StatusOK {
		t.Fatalf("end status = %d, want %d", endRes.StatusCode, http.StatusOK)
	}
}

func TestUIRoutes(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_ui_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	rootRes, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer rootRes.Body.Close()
	if rootRes.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("GET / status = %d, want %d", rootRes.StatusCode, http.StatusTemporaryRedirect)
	}
	if got := rootRes.Header.Get("Location"); got != "/ui/" {
		t.Fatalf("GET / location = %q, want %q", got, "/ui/")
	}

	uiRes, err := http.Get(ts.URL + "/ui/")
	if err != nil {
		t.Fatalf("GET /ui/ error = %v", err)
	}
	defer uiRes.Body.Close()
	if uiRes.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/ status = %d, want %d", uiRes.StatusCode, http.StatusOK)
	}

	var body bytes.Buffer
	if _, err := body.ReadFrom(uiRes.Body); err != nil {
		t.Fatalf("reading /ui/ body failed: %v", err)
	}
	if !strings.Contains(body.String(), "id=\"pulse\"") {
		t.Fatalf("GET /ui/ body missing expected content")
	}
}

func TestOnboardingStatus(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
		VoiceProvider:            "mock",
		OpenClawAdapterMode:      "mock",
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_onboarding_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/v1/onboarding/status")
	if err != nil {
		t.Fatalf("GET /v1/onboarding/status error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["voice_provider"] != "mock" {
		t.Fatalf("voice_provider = %v, want %v", payload["voice_provider"], "mock")
	}
	if payload["brain_provider"] != "mock" {
		t.Fatalf("brain_provider = %v, want %v", payload["brain_provider"], "mock")
	}
	if _, ok := payload["checks"]; !ok {
		t.Fatalf("missing checks in response: %+v", payload)
	}
}
