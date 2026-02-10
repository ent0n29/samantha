package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ent0n29/samantha/internal/config"
	"github.com/ent0n29/samantha/internal/observability"
	"github.com/ent0n29/samantha/internal/session"
)

func TestCreateAndEndSession(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics)
	router := srv.Router()

	createReq := map[string]string{
		"user_id":    "user-1",
		"persona_id": "warm",
	}
	body, _ := json.Marshal(createReq)
	createRes := doRequest(t, router, http.MethodPost, "/v1/voice/session", "application/json", body)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRes.Code, http.StatusCreated)
	}

	var created map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("missing session_id in create response: %+v", created)
	}

	endRes := doRequest(t, router, http.MethodPost, "/v1/voice/session/"+sessionID+"/end", "application/json", nil)
	if endRes.Code != http.StatusOK {
		t.Fatalf("end status = %d, want %d", endRes.Code, http.StatusOK)
	}
}

func TestUIRoutes(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_ui_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics)
	router := srv.Router()

	rootRes := doRequest(t, router, http.MethodGet, "/", "", nil)
	if rootRes.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET / status = %d, want %d", rootRes.Code, http.StatusTemporaryRedirect)
	}
	if got := rootRes.Header().Get("Location"); got != "/ui/" {
		t.Fatalf("GET / location = %q, want %q", got, "/ui/")
	}

	uiRes := doRequest(t, router, http.MethodGet, "/ui/", "", nil)
	if uiRes.Code != http.StatusOK {
		t.Fatalf("GET /ui/ status = %d, want %d", uiRes.Code, http.StatusOK)
	}
	if !strings.Contains(uiRes.Body.String(), "id=\"pulse\"") {
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
	router := srv.Router()

	res := doRequest(t, router, http.MethodGet, "/v1/onboarding/status", "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
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

func doRequest(t *testing.T, handler http.Handler, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
