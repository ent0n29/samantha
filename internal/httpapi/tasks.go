package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ent0n29/samantha/internal/tasks"
)

type createTaskRequest struct {
	SessionID  string `json:"session_id"`
	UserID     string `json:"user_id"`
	IntentText string `json:"intent_text"`
	Mode       string `json:"mode"`
	Priority   string `json:"priority"`
}

type createTaskResponse struct {
	TaskID           string `json:"task_id"`
	Status           string `json:"status"`
	RequiresApproval bool   `json:"requires_approval"`
	Summary          string `json:"summary"`
	Deduped          bool   `json:"deduped"`
}

type approveTaskRequest struct {
	Approved *bool  `json:"approved"`
	Scope    string `json:"scope"`
}

type cancelTaskRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if s.taskService == nil || !s.taskService.Enabled() {
		respondError(w, http.StatusNotImplemented, "task_runtime_disabled", "Task runtime is disabled.")
		return
	}

	var req createTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.IntentText = strings.TrimSpace(req.IntentText)
	req.Mode = strings.TrimSpace(req.Mode)
	req.Priority = strings.TrimSpace(req.Priority)

	if req.SessionID == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "session_id is required")
		return
	}
	if req.IntentText == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "intent_text is required")
		return
	}
	sess, err := s.sessions.Get(req.SessionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "session_not_found", err.Error())
		return
	}
	if req.UserID == "" {
		req.UserID = sess.UserID
		if req.UserID == "" {
			req.UserID = "anonymous"
		}
	}
	if req.Mode == "" {
		req.Mode = "auto"
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}

	task, deduped, err := s.taskService.CreateTask(r.Context(), tasks.CreateRequest{
		SessionID:  req.SessionID,
		UserID:     req.UserID,
		IntentText: req.IntentText,
		Mode:       req.Mode,
		Priority:   req.Priority,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, "task_create_failed", err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, createTaskResponse{
		TaskID:           task.ID,
		Status:           string(task.Status),
		RequiresApproval: task.RequiresApproval,
		Summary:          task.Summary,
		Deduped:          deduped,
	})
}

func (s *Server) handleApproveTask(w http.ResponseWriter, r *http.Request) {
	if s.taskService == nil || !s.taskService.Enabled() {
		respondError(w, http.StatusNotImplemented, "task_runtime_disabled", "Task runtime is disabled.")
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	if taskID == "" {
		respondError(w, http.StatusBadRequest, "invalid_task_id", "missing task id")
		return
	}

	var req approveTaskRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	approved := true
	if req.Approved != nil {
		approved = *req.Approved
	}

	task, err := s.taskService.ApproveTask(r.Context(), taskID, approved)
	if err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			respondError(w, http.StatusNotFound, "task_not_found", err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, "task_approval_failed", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	if s.taskService == nil || !s.taskService.Enabled() {
		respondError(w, http.StatusNotImplemented, "task_runtime_disabled", "Task runtime is disabled.")
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	if taskID == "" {
		respondError(w, http.StatusBadRequest, "invalid_task_id", "missing task id")
		return
	}

	task, err := s.taskService.GetTask(taskID)
	if err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			respondError(w, http.StatusNotFound, "task_not_found", err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, "task_get_failed", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, task)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if s.taskService == nil || !s.taskService.Enabled() {
		respondError(w, http.StatusNotImplemented, "task_runtime_disabled", "Task runtime is disabled.")
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	if taskID == "" {
		respondError(w, http.StatusBadRequest, "invalid_task_id", "missing task id")
		return
	}

	reason := "Cancelled by API."
	var req cancelTaskRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Reason) != "" {
		reason = strings.TrimSpace(req.Reason)
	}

	task, err := s.taskService.CancelTask(r.Context(), taskID, reason)
	if err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			respondError(w, http.StatusNotFound, "task_not_found", err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, "task_cancel_failed", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, task)
}

func (s *Server) handleListTaskEvents(w http.ResponseWriter, r *http.Request) {
	if s.taskService == nil || !s.taskService.Enabled() {
		respondError(w, http.StatusNotImplemented, "task_runtime_disabled", "Task runtime is disabled.")
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	if taskID == "" {
		respondError(w, http.StatusBadRequest, "invalid_task_id", "missing task id")
		return
	}

	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			respondError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return
		}
		if n > 500 {
			n = 500
		}
		limit = n
	}

	events, err := s.taskService.ListTaskEvents(taskID, limit)
	if err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			respondError(w, http.StatusNotFound, "task_not_found", err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, "task_events_failed", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"task_id": taskID,
		"events":  events,
	})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if s.taskService == nil || !s.taskService.Enabled() {
		respondError(w, http.StatusNotImplemented, "task_runtime_disabled", "Task runtime is disabled.")
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "session_id query param is required")
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			respondError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"tasks":      s.taskService.ListTasks(sessionID, limit),
	})
}
