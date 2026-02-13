package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/ent0n29/samantha/internal/config"
	"github.com/ent0n29/samantha/internal/observability"
	"github.com/ent0n29/samantha/internal/protocol"
	"github.com/ent0n29/samantha/internal/session"
	"github.com/ent0n29/samantha/internal/taskruntime"
)

type Orchestrator interface {
	RunConnection(ctx context.Context, s *session.Session, inbound <-chan any, outbound chan<- any) error
	PreviewTTS(ctx context.Context, voiceID, modelID, personaID, text string) ([]byte, string, error)
}

type Server struct {
	cfg          config.Config
	sessions     *session.Manager
	orchestrator Orchestrator
	taskService  *taskruntime.Service
	metrics      *observability.Metrics
	upgrader     websocket.Upgrader
	static       http.Handler
}

func New(cfg config.Config, sessions *session.Manager, orchestrator Orchestrator, metrics *observability.Metrics, taskService *taskruntime.Service) *Server {
	return &Server{
		cfg:          cfg,
		sessions:     sessions,
		orchestrator: orchestrator,
		taskService:  taskService,
		metrics:      metrics,
		static:       newStaticHandler(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				// Default: only allow browser websocket connections from the same origin.
				// This prevents other websites from driving the user's mic session if
				// Samantha is ever exposed beyond localhost.
				if cfg.AllowAnyOrigin {
					return true
				}
				origin := strings.TrimSpace(r.Header.Get("Origin"))
				if origin == "" {
					// Non-browser clients often omit Origin. Allow them.
					return true
				}
				u, err := url.Parse(origin)
				if err != nil {
					return false
				}
				if u.Scheme != "http" && u.Scheme != "https" {
					return false
				}
				return strings.EqualFold(u.Host, r.Host)
			},
		},
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})
	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})
	r.Handle("/ui/*", http.StripPrefix("/ui/", s.static))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		observability.MetricsHandler().ServeHTTP(w, r)
	})

	r.Post("/v1/voice/session", s.handleCreateSession)
	r.Post("/v1/voice/session/{id}/end", s.handleEndSession)
	r.Get("/v1/voice/session/ws", s.handleSessionWS)
	r.Get("/v1/onboarding/status", s.handleOnboardingStatus)
	r.Get("/v1/perf/latency", s.handlePerfLatency)
	r.Get("/v1/voice/voices", s.handleListVoices)
	r.Post("/v1/voice/tts/preview", s.handlePreviewTTS)
	r.Post("/v1/tasks", s.handleCreateTask)
	r.Post("/v1/tasks/{id}/approve", s.handleApproveTask)
	r.Post("/v1/tasks/{id}/cancel", s.handleCancelTask)
	r.Get("/v1/tasks/{id}", s.handleGetTask)
	r.Get("/v1/tasks/{id}/events", s.handleListTaskEvents)
	r.Get("/v1/tasks", s.handleListTasks)

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"status":               "ok",
		"task_runtime_enabled": s.taskService != nil && s.taskService.Enabled(),
		"task_store_mode":      s.taskStoreMode(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"status":               "ready",
		"task_runtime_enabled": s.taskService != nil && s.taskService.Enabled(),
		"task_store_mode":      s.taskStoreMode(),
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req session.CreateRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.UserID) == "" {
		req.UserID = "anonymous"
	}
	if strings.TrimSpace(req.PersonaID) == "" {
		req.PersonaID = "warm"
	}
	if strings.TrimSpace(req.VoiceID) == "" {
		defaultVoice := s.cfg.ElevenLabsTTSVoice
		if strings.EqualFold(strings.TrimSpace(s.cfg.VoiceProvider), "local") {
			defaultVoice = s.cfg.LocalKokoroVoice
			if strings.TrimSpace(defaultVoice) == "" {
				defaultVoice = "af_heart"
			}
		}
		req.VoiceID = defaultVoice
	}

	sess := s.sessions.Create(req.UserID, req.PersonaID, req.VoiceID)
	s.metrics.ActiveSessions.Set(float64(s.sessions.ActiveCount()))
	s.metrics.SessionEvents.WithLabelValues("created").Inc()

	respondJSON(w, http.StatusCreated, session.CreateResponse{
		SessionID:       sess.ID,
		UserID:          sess.UserID,
		Status:          sess.Status,
		PersonaID:       sess.PersonaID,
		VoiceID:         sess.VoiceID,
		StartedAt:       sess.StartedAt,
		LastActivityAt:  sess.LastActivityAt,
		InactivityTTLMS: s.cfg.SessionInactivityTimeout.Milliseconds(),
	})
}

func (s *Server) handleEndSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		respondError(w, http.StatusBadRequest, "invalid_session_id", "missing session id")
		return
	}

	sess, err := s.sessions.End(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "session_not_found", err.Error())
		return
	}
	s.metrics.ActiveSessions.Set(float64(s.sessions.ActiveCount()))
	s.metrics.SessionEvents.WithLabelValues("ended").Inc()
	respondJSON(w, http.StatusOK, sess)
}

func (s *Server) handleSessionWS(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "missing_session_id", "query parameter session_id is required")
		return
	}
	if s.orchestrator == nil {
		respondError(w, http.StatusNotImplemented, "unavailable", "orchestrator not configured")
		return
	}

	sess, err := s.sessions.Get(sessionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "session_not_found", err.Error())
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	s.metrics.SessionEvents.WithLabelValues("ws_connected").Inc()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	inbound := make(chan any, 256)
	outbound := make(chan any, 256)
	runDone := make(chan struct{})

	go func() {
		defer close(runDone)
		_ = s.orchestrator.RunConnection(ctx, sess, inbound, outbound)
	}()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-outbound:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(msg); err != nil {
					s.metrics.WSWriteErrors.WithLabelValues("write_json").Inc()
					cancel()
					return
				}
				t, ok := messageTypeOf(msg)
				if ok {
					s.metrics.WSMessages.WithLabelValues("outbound", string(t)).Inc()
				}
			}
		}
	}()

	conn.SetReadLimit(2 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})

readLoop:
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType != websocket.TextMessage {
			continue
		}
		parsed, err := protocol.ParseClientMessage(data)
		if err != nil {
			errEvent := protocol.ErrorEvent{
				Type:      protocol.TypeErrorEvent,
				SessionID: sessionID,
				Code:      "invalid_client_message",
				Source:    "gateway",
				Retryable: false,
				Detail:    err.Error(),
			}
			select {
			case outbound <- errEvent:
				s.metrics.ObserveOutboundMessage(string(protocol.TypeErrorEvent), "queued")
			default:
				// Keep websocket writes single-threaded; drop if outbound queue is saturated.
				s.metrics.ObserveOutboundMessage(string(protocol.TypeErrorEvent), "drop_full")
			}
			continue
		}

		if t, ok := messageTypeOf(parsed); ok {
			s.metrics.WSMessages.WithLabelValues("inbound", string(t)).Inc()
		}
		select {
		case <-ctx.Done():
			break readLoop
		case inbound <- parsed:
		}
	}

	cancel()
	close(inbound)
	<-runDone
	<-writerDone
	s.metrics.SessionEvents.WithLabelValues("ws_disconnected").Inc()
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

var errEmptyBody = errors.New("empty body")

func decodeJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return errEmptyBody
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(out); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "eof") {
			return errEmptyBody
		}
		return err
	}
	return nil
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func respondError(w http.ResponseWriter, status int, code, message string) {
	respondJSON(w, status, errorResponse{Error: message, Code: code})
}

func (s *Server) taskStoreMode() string {
	if s.taskService == nil {
		if s.cfg.TaskRuntimeEnabled {
			return "in-memory"
		}
		return "disabled"
	}
	mode := strings.TrimSpace(s.taskService.StoreMode())
	if mode == "" {
		return "disabled"
	}
	return mode
}

func messageTypeOf(v any) (protocol.MessageType, bool) {
	switch m := v.(type) {
	case protocol.ClientAudioChunk:
		return m.Type, true
	case protocol.ClientControl:
		return m.Type, true
	case protocol.STTPartial:
		return m.Type, true
	case protocol.STTCommitted:
		return m.Type, true
	case protocol.AssistantTextDelta:
		return m.Type, true
	case protocol.AssistantAudioChunk:
		return m.Type, true
	case protocol.AssistantTurnEnd:
		return m.Type, true
	case protocol.SystemEvent:
		return m.Type, true
	case protocol.ErrorEvent:
		return m.Type, true
	case protocol.TaskCreated:
		return m.Type, true
	case protocol.TaskPlanDelta:
		return m.Type, true
	case protocol.TaskStepStarted:
		return m.Type, true
	case protocol.TaskStepLog:
		return m.Type, true
	case protocol.TaskStepCompleted:
		return m.Type, true
	case protocol.TaskWaitingApproval:
		return m.Type, true
	case protocol.TaskCompleted:
		return m.Type, true
	case protocol.TaskFailed:
		return m.Type, true
	case protocol.TaskStatusSnapshot:
		return m.Type, true
	default:
		return "", false
	}
}
