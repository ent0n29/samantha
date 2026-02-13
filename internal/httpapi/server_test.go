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
	"github.com/ent0n29/samantha/internal/openclaw"
	"github.com/ent0n29/samantha/internal/session"
	"github.com/ent0n29/samantha/internal/taskruntime"
)

func TestCreateAndEndSession(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics, nil)
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
	srv := New(cfg, sessions, nil, metrics, nil)
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
	if !strings.Contains(uiRes.Body.String(), "id=\"taskDesk\"") {
		t.Fatalf("GET /ui/ body missing task desk")
	}

	workletRes := doRequest(t, router, http.MethodGet, "/ui/mic-worklet.js", "", nil)
	if workletRes.Code != http.StatusOK {
		t.Fatalf("GET /ui/mic-worklet.js status = %d, want %d", workletRes.Code, http.StatusOK)
	}
	if !strings.Contains(workletRes.Body.String(), "registerProcessor") {
		t.Fatalf("GET /ui/mic-worklet.js missing expected content")
	}
}

func TestOnboardingStatus(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
		VoiceProvider:            "mock",
		OpenClawAdapterMode:      "mock",
		UIAudioWorklet:           true,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_onboarding_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics, nil)
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
	if payload["task_runtime_enabled"] != false {
		t.Fatalf("task_runtime_enabled = %v, want %v", payload["task_runtime_enabled"], false)
	}
	if payload["task_store_mode"] != "disabled" {
		t.Fatalf("task_store_mode = %v, want %v", payload["task_store_mode"], "disabled")
	}
	if payload["ui_audio_worklet"] != true {
		t.Fatalf("ui_audio_worklet = %v, want %v", payload["ui_audio_worklet"], true)
	}
	if _, ok := payload["checks"]; !ok {
		t.Fatalf("missing checks in response: %+v", payload)
	}
}

func TestUISettings(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
		UIAudioWorklet:           true,
		UITaskDeskDefault:        false,
		UISilenceBreakerMode:     "visual",
		UISilenceBreakerDelay:    900 * time.Millisecond,
		TaskRuntimeEnabled:       true,
		TaskTimeout:              5 * time.Minute,
		TaskIdempotencyWindow:    10 * time.Second,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_ui_settings_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	taskService := taskruntime.New(taskruntime.Config{
		Enabled:           true,
		TaskTimeout:       cfg.TaskTimeout,
		IdempotencyWindow: cfg.TaskIdempotencyWindow,
	}, openclaw.NewMockAdapter(), metrics)
	defer func() { _ = taskService.Close() }()

	srv := New(cfg, sessions, nil, metrics, taskService)
	router := srv.Router()

	res := doRequest(t, router, http.MethodGet, "/v1/ui/settings", "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["ui_audio_worklet"] != true {
		t.Fatalf("ui_audio_worklet = %v, want true", payload["ui_audio_worklet"])
	}
	if payload["task_runtime_enabled"] != true {
		t.Fatalf("task_runtime_enabled = %v, want true", payload["task_runtime_enabled"])
	}
	if payload["task_desk_default"] != false {
		t.Fatalf("task_desk_default = %v, want false", payload["task_desk_default"])
	}
	if payload["silence_breaker_mode"] != "visual" {
		t.Fatalf("silence_breaker_mode = %v, want visual", payload["silence_breaker_mode"])
	}
	if payload["silence_breaker_delay_ms"] != float64(900) {
		t.Fatalf("silence_breaker_delay_ms = %v, want 900", payload["silence_breaker_delay_ms"])
	}
}

func TestPerfLatencySnapshot(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_perf_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	metrics.ObserveTurnStage("commit_to_first_text", 320*time.Millisecond)
	metrics.ObserveTurnStage("commit_to_first_text", 410*time.Millisecond)
	metrics.ObserveTurnStage("commit_to_first_audio", 780*time.Millisecond)

	srv := New(cfg, sessions, nil, metrics, nil)
	router := srv.Router()
	res := doRequest(t, router, http.MethodGet, "/v1/perf/latency", "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	stagesAny, ok := payload["stages"].([]any)
	if !ok || len(stagesAny) == 0 {
		t.Fatalf("stages = %T (%v), want non-empty array", payload["stages"], payload["stages"])
	}

	found := false
	for _, item := range stagesAny {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["stage"] == "commit_to_first_text" {
			found = true
			samples, ok := m["samples"].(float64)
			if !ok {
				t.Fatalf("samples type = %T, want number", m["samples"])
			}
			if got := int(samples); got < 2 {
				t.Fatalf("samples = %d, want >=2", got)
			}
			break
		}
	}
	if !found {
		t.Fatalf("missing commit_to_first_text stage in %+v", stagesAny)
	}
}

func TestPerfLatencyReset(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_perf_reset_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	metrics.ObserveTurnStage("commit_to_first_text", 320*time.Millisecond)
	metrics.ObserveTurnStage("commit_to_first_audio", 780*time.Millisecond)

	srv := New(cfg, sessions, nil, metrics, nil)
	router := srv.Router()

	before := doRequest(t, router, http.MethodGet, "/v1/perf/latency", "", nil)
	if before.Code != http.StatusOK {
		t.Fatalf("before status = %d, want %d", before.Code, http.StatusOK)
	}
	var beforePayload map[string]any
	if err := json.Unmarshal(before.Body.Bytes(), &beforePayload); err != nil {
		t.Fatalf("decode before response: %v", err)
	}
	beforeStages, _ := beforePayload["stages"].([]any)
	if len(beforeStages) == 0 {
		t.Fatalf("before stages empty, want populated")
	}

	reset := doRequest(t, router, http.MethodPost, "/v1/perf/latency/reset", "", nil)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want %d", reset.Code, http.StatusOK)
	}

	after := doRequest(t, router, http.MethodGet, "/v1/perf/latency", "", nil)
	if after.Code != http.StatusOK {
		t.Fatalf("after status = %d, want %d", after.Code, http.StatusOK)
	}
	var afterPayload map[string]any
	if err := json.Unmarshal(after.Body.Bytes(), &afterPayload); err != nil {
		t.Fatalf("decode after response: %v", err)
	}
	afterStages, _ := afterPayload["stages"].([]any)
	if len(afterStages) != 0 {
		t.Fatalf("after stages len = %d, want 0", len(afterStages))
	}
}

func TestTaskEndpointsDisabledByDefault(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_tasks_disabled_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	srv := New(cfg, sessions, nil, metrics, nil)
	router := srv.Router()

	res := doRequest(t, router, http.MethodGet, "/v1/tasks?session_id=s1", "", nil)
	if res.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotImplemented)
	}
}

func TestTaskEndpointsCreateApproveAndGet(t *testing.T) {
	cfg := config.Config{
		SessionInactivityTimeout: 2 * time.Minute,
		TaskRuntimeEnabled:       true,
		TaskTimeout:              5 * time.Minute,
		TaskIdempotencyWindow:    10 * time.Second,
	}
	sessions := session.NewManager(cfg.SessionInactivityTimeout)
	metrics := observability.NewMetrics("test_httpapi_tasks_enabled_" + time.Now().Format("150405") + "_" + time.Now().Format("000000000"))
	taskService := taskruntime.New(taskruntime.Config{
		Enabled:           true,
		TaskTimeout:       cfg.TaskTimeout,
		IdempotencyWindow: cfg.TaskIdempotencyWindow,
	}, openclaw.NewMockAdapter(), metrics)
	defer func() { _ = taskService.Close() }()

	srv := New(cfg, sessions, nil, metrics, taskService)
	router := srv.Router()

	createSessionReq := map[string]any{
		"user_id":    "u-task-1",
		"persona_id": "warm",
	}
	createSessionBody, _ := json.Marshal(createSessionReq)
	sessionRes := doRequest(t, router, http.MethodPost, "/v1/voice/session", "application/json", createSessionBody)
	if sessionRes.Code != http.StatusCreated {
		t.Fatalf("session create status = %d, want %d body=%s", sessionRes.Code, http.StatusCreated, sessionRes.Body.String())
	}
	var sessionPayload map[string]any
	if err := json.Unmarshal(sessionRes.Body.Bytes(), &sessionPayload); err != nil {
		t.Fatalf("decode session create: %v", err)
	}
	sessionID, _ := sessionPayload["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("missing session id in payload: %+v", sessionPayload)
	}

	createReq := map[string]any{
		"session_id":  sessionID,
		"user_id":     "u-task-1",
		"intent_text": "deploy release alpha",
	}
	createBody, _ := json.Marshal(createReq)
	createRes := doRequest(t, router, http.MethodPost, "/v1/tasks", "application/json", createBody)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d body=%s", createRes.Code, http.StatusCreated, createRes.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	taskID, _ := created["task_id"].(string)
	if taskID == "" {
		t.Fatalf("missing task_id in create response: %+v", created)
	}

	approveReq := map[string]any{"approved": true}
	approveBody, _ := json.Marshal(approveReq)
	approveRes := doRequest(t, router, http.MethodPost, "/v1/tasks/"+taskID+"/approve", "application/json", approveBody)
	if approveRes.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d body=%s", approveRes.Code, http.StatusOK, approveRes.Body.String())
	}

	getRes := doRequest(t, router, http.MethodGet, "/v1/tasks/"+taskID, "", nil)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d body=%s", getRes.Code, http.StatusOK, getRes.Body.String())
	}

	createReqTwo := map[string]any{
		"session_id":  sessionID,
		"user_id":     "u-task-1",
		"intent_text": "deploy release beta",
	}
	createTwoBody, _ := json.Marshal(createReqTwo)
	createTwoRes := doRequest(t, router, http.MethodPost, "/v1/tasks", "application/json", createTwoBody)
	if createTwoRes.Code != http.StatusCreated {
		t.Fatalf("create task two status = %d, want %d body=%s", createTwoRes.Code, http.StatusCreated, createTwoRes.Body.String())
	}
	var createdTwo map[string]any
	if err := json.Unmarshal(createTwoRes.Body.Bytes(), &createdTwo); err != nil {
		t.Fatalf("decode create task two: %v", err)
	}
	taskTwoID, _ := createdTwo["task_id"].(string)
	if taskTwoID == "" {
		t.Fatalf("missing task_id in second create response: %+v", createdTwo)
	}

	cancelReq := map[string]any{"reason": "cancelled in test"}
	cancelBody, _ := json.Marshal(cancelReq)
	cancelRes := doRequest(t, router, http.MethodPost, "/v1/tasks/"+taskTwoID+"/cancel", "application/json", cancelBody)
	if cancelRes.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want %d body=%s", cancelRes.Code, http.StatusOK, cancelRes.Body.String())
	}

	getCancelledRes := doRequest(t, router, http.MethodGet, "/v1/tasks/"+taskTwoID, "", nil)
	if getCancelledRes.Code != http.StatusOK {
		t.Fatalf("get cancelled status = %d, want %d body=%s", getCancelledRes.Code, http.StatusOK, getCancelledRes.Body.String())
	}
	var cancelledTask map[string]any
	if err := json.Unmarshal(getCancelledRes.Body.Bytes(), &cancelledTask); err != nil {
		t.Fatalf("decode cancelled task: %v", err)
	}
	if cancelledTask["status"] != "cancelled" {
		t.Fatalf("cancelled task status = %v, want cancelled", cancelledTask["status"])
	}

	eventsRes := doRequest(t, router, http.MethodGet, "/v1/tasks/"+taskTwoID+"/events?limit=50", "", nil)
	if eventsRes.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d body=%s", eventsRes.Code, http.StatusOK, eventsRes.Body.String())
	}
	var eventsPayload map[string]any
	if err := json.Unmarshal(eventsRes.Body.Bytes(), &eventsPayload); err != nil {
		t.Fatalf("decode events payload: %v", err)
	}
	eventsAny, ok := eventsPayload["events"].([]any)
	if !ok || len(eventsAny) == 0 {
		t.Fatalf("events payload = %T (%v), want non-empty array", eventsPayload["events"], eventsPayload["events"])
	}

	listRes := doRequest(t, router, http.MethodGet, "/v1/tasks?session_id="+sessionID, "", nil)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d body=%s", listRes.Code, http.StatusOK, listRes.Body.String())
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
