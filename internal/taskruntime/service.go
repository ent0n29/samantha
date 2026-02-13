package taskruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ent0n29/samantha/internal/execution"
	"github.com/ent0n29/samantha/internal/observability"
	"github.com/ent0n29/samantha/internal/openclaw"
	"github.com/ent0n29/samantha/internal/policy"
	"github.com/ent0n29/samantha/internal/tasks"
)

type Config struct {
	Enabled           bool
	TaskTimeout       time.Duration
	IdempotencyWindow time.Duration
	DatabaseURL       string
}

type Service struct {
	enabled     bool
	storeMode   string
	taskTimeout time.Duration
	manager     *tasks.Manager
	runner      *execution.Runner
	store       tasks.Store
	metrics     *observability.Metrics

	mu             sync.Mutex
	runningCancels map[string]context.CancelFunc
}

func New(cfg Config, adapter openclaw.Adapter, metrics *observability.Metrics) *Service {
	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = 20 * time.Minute
	}
	if cfg.IdempotencyWindow <= 0 {
		cfg.IdempotencyWindow = 10 * time.Second
	}

	var runner *execution.Runner
	if adapter != nil {
		runner = execution.NewRunner(adapter)
	}

	manager := tasks.NewManager(cfg.IdempotencyWindow)
	var store tasks.Store
	storeMode := "disabled"
	if cfg.Enabled {
		storeMode = "in-memory"
		if st, err := tasks.NewStore(context.Background(), cfg.DatabaseURL); err == nil {
			store = st
			if store != nil {
				manager.SetStore(store)
				storeMode = "postgres"
			}
		}
	}

	return &Service{
		enabled:        cfg.Enabled,
		storeMode:      storeMode,
		taskTimeout:    cfg.TaskTimeout,
		manager:        manager,
		runner:         runner,
		store:          store,
		metrics:        metrics,
		runningCancels: make(map[string]context.CancelFunc),
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.enabled
}

func (s *Service) StoreMode() string {
	if s == nil {
		return "disabled"
	}
	return s.storeMode
}

func (s *Service) Subscribe(sessionID string) (<-chan tasks.Event, func()) {
	if s == nil {
		ch := make(chan tasks.Event)
		close(ch)
		return ch, func() {}
	}
	return s.manager.Subscribe(sessionID)
}

func (s *Service) CreateTask(ctx context.Context, req tasks.CreateRequest) (tasks.Task, bool, error) {
	_ = ctx
	if !s.Enabled() {
		return tasks.Task{}, false, errors.New("task runtime is disabled")
	}

	decision := policy.DecideIntent(req.IntentText)
	if decision.Blocked {
		return tasks.Task{}, false, fmt.Errorf("blocked by policy: %s", strings.TrimSpace(decision.Reason))
	}

	risk := mapRisk(decision.Risk)
	task, dedup, startTaskID, err := s.manager.Create(req, summarizeIntent(req.IntentText), risk, decision.RequiresApproval)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if s.metrics != nil && !dedup {
		s.metrics.ObserveTaskEvent("created")
	}

	if startTaskID != "" {
		s.startTask(startTaskID)
	}
	return task, dedup, nil
}

func (s *Service) MaybeCreateFromUtterance(ctx context.Context, sessionID, userID, utterance string) (tasks.Task, bool, error) {
	if !s.Enabled() {
		return tasks.Task{}, false, nil
	}
	if !policy.LooksActionableIntent(utterance) {
		return tasks.Task{}, false, nil
	}
	task, _, err := s.CreateTask(ctx, tasks.CreateRequest{
		SessionID:  sessionID,
		UserID:     userID,
		IntentText: utterance,
		Mode:       "auto",
		Priority:   "normal",
	})
	if err != nil {
		return tasks.Task{}, false, err
	}
	return task, true, nil
}

func (s *Service) ApproveTask(ctx context.Context, taskID string, approved bool) (tasks.Task, error) {
	_ = ctx
	if !s.Enabled() {
		return tasks.Task{}, errors.New("task runtime is disabled")
	}
	task, startTaskID, err := s.manager.Approve(taskID, approved)
	if err != nil {
		return tasks.Task{}, err
	}
	if s.metrics != nil {
		if approved {
			s.metrics.ObserveTaskEvent("approved")
			if !task.CreatedAt.IsZero() {
				s.metrics.ObserveTaskApprovalWait(time.Since(task.CreatedAt))
			}
		} else {
			s.metrics.ObserveTaskEvent("denied")
		}
	}
	if startTaskID != "" {
		s.startTask(startTaskID)
	}
	return task, nil
}

func (s *Service) ApproveLatestForSession(ctx context.Context, sessionID string, approved bool) (tasks.Task, error) {
	_ = ctx
	if !s.Enabled() {
		return tasks.Task{}, errors.New("task runtime is disabled")
	}
	task, err := s.manager.LatestAwaitingApproval(sessionID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.ApproveTask(context.Background(), task.ID, approved)
}

func (s *Service) CancelTask(ctx context.Context, taskID, reason string) (tasks.Task, error) {
	_ = ctx
	if !s.Enabled() {
		return tasks.Task{}, errors.New("task runtime is disabled")
	}
	if cancel := s.getRunningCancel(taskID); cancel != nil {
		cancel()
	}
	task, nextTaskID, err := s.manager.Cancel(taskID, reason)
	if err != nil {
		return tasks.Task{}, err
	}
	if s.metrics != nil {
		s.metrics.ObserveTaskEvent("cancelled")
	}
	if nextTaskID != "" {
		s.startTask(nextTaskID)
	}
	return task, nil
}

func (s *Service) CancelActiveForSession(ctx context.Context, sessionID, reason string) (tasks.Task, error) {
	_ = ctx
	if !s.Enabled() {
		return tasks.Task{}, errors.New("task runtime is disabled")
	}
	task, err := s.manager.ActiveTask(sessionID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.CancelTask(context.Background(), task.ID, reason)
}

func (s *Service) GetTask(taskID string) (tasks.Task, error) {
	if s == nil {
		return tasks.Task{}, errors.New("task runtime unavailable")
	}
	return s.manager.Get(taskID)
}

func (s *Service) ListTasks(sessionID string, limit int) []tasks.Task {
	if s == nil {
		return nil
	}
	return s.manager.ListBySession(sessionID, limit)
}

func (s *Service) ListTaskEvents(taskID string, limit int) ([]tasks.Event, error) {
	if s == nil {
		return nil, errors.New("task runtime unavailable")
	}
	return s.manager.ListEvents(taskID, limit)
}

func (s *Service) startTask(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	task, err := s.manager.Get(taskID)
	if err != nil || task.Status != tasks.TaskStatusRunning {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.taskTimeout)
	s.setRunningCancel(taskID, cancel)

	go func(task tasks.Task, doneCancel context.CancelFunc) {
		defer doneCancel()
		defer s.clearRunningCancel(task.ID)

		if s.runner == nil {
			_, nextTaskID, _ := s.manager.Fail(task.ID, "execution_unavailable", "Task runner is not configured.")
			if s.metrics != nil {
				s.metrics.ObserveTaskEvent("failed")
			}
			if nextTaskID != "" {
				s.startTask(nextTaskID)
			}
			return
		}

		output, runErr := s.runner.RunTask(ctx, task, func(delta string) error {
			return s.manager.AppendStepLog(task.ID, delta)
		})
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				_, nextTaskID, _ := s.manager.Cancel(task.ID, "Task cancelled.")
				if s.metrics != nil {
					s.metrics.ObserveTaskEvent("cancelled")
				}
				if nextTaskID != "" {
					s.startTask(nextTaskID)
				}
				return
			}

			_, nextTaskID, _ := s.manager.Fail(task.ID, "execution_failed", runErr.Error())
			if s.metrics != nil {
				s.metrics.ObserveTaskEvent("failed")
			}
			if nextTaskID != "" {
				s.startTask(nextTaskID)
			}
			return
		}

		_, nextTaskID, _ := s.manager.Complete(task.ID, output)
		if s.metrics != nil {
			s.metrics.ObserveTaskEvent("completed")
			if task.StartedAt != nil {
				s.metrics.ObserveTaskStepLatency(time.Since(*task.StartedAt))
			}
		}
		if nextTaskID != "" {
			s.startTask(nextTaskID)
		}
	}(task, cancel)
}

func (s *Service) setRunningCancel(taskID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runningCancels[taskID] = cancel
}

func (s *Service) getRunningCancel(taskID string) context.CancelFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runningCancels[taskID]
}

func (s *Service) clearRunningCancel(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runningCancels, taskID)
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func mapRisk(risk string) tasks.RiskLevel {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "blocked":
		return tasks.RiskLevelBlocked
	case "high":
		return tasks.RiskLevelHigh
	case "medium":
		return tasks.RiskLevelMedium
	default:
		return tasks.RiskLevelLow
	}
}

func summarizeIntent(intent string) string {
	s := strings.TrimSpace(intent)
	if s == "" {
		return "Task"
	}
	if len(s) <= 120 {
		return s
	}
	s = s[:120]
	lastSpace := strings.LastIndexByte(s, ' ')
	if lastSpace > 70 {
		s = s[:lastSpace]
	}
	return strings.TrimSpace(s) + "..."
}
