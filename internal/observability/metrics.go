package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics groups all Prometheus instruments used by the service.
type Metrics struct {
	ActiveSessions    prometheus.Gauge
	SessionEvents     *prometheus.CounterVec
	TaskEvents        *prometheus.CounterVec
	WSMessages        *prometheus.CounterVec
	WSWriteErrors     *prometheus.CounterVec
	OutboundMessages  *prometheus.CounterVec
	ProviderErrors    *prometheus.CounterVec
	FirstAudioLatency prometheus.Histogram
	TurnStageLatency  *prometheus.HistogramVec
	TaskStepLatency   prometheus.Histogram
	TaskApprovalWait  prometheus.Histogram
	turnStageWindow   *turnStageWindow
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		ActiveSessions: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_sessions",
			Help:      "Number of active realtime voice sessions.",
		}),
		SessionEvents: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "session_events_total",
			Help:      "Session events by type.",
		}, []string{"event"}),
		TaskEvents: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_events_total",
			Help:      "Task runtime events by type.",
		}, []string{"event"}),
		WSMessages: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ws_messages_total",
			Help:      "WebSocket messages by direction and type.",
		}, []string{"direction", "type"}),
		WSWriteErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ws_write_errors_total",
			Help:      "WebSocket write errors by reason.",
		}, []string{"reason"}),
		OutboundMessages: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "outbound_messages_total",
			Help:      "Outbound orchestrator messages by type and delivery result.",
		}, []string{"type", "result"}),
		ProviderErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "provider_errors_total",
			Help:      "Provider errors by provider and code.",
		}, []string{"provider", "code"}),
		FirstAudioLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "first_audio_latency_ms",
			Help:      "Latency to first assistant audio chunk in milliseconds.",
			Buckets:   []float64{100, 200, 300, 500, 700, 900, 1200, 2000},
		}),
		TurnStageLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "turn_stage_latency_ms",
			Help:      "Turn-stage latency in milliseconds.",
			Buckets:   []float64{20, 50, 100, 150, 250, 400, 700, 1200, 2000, 4000, 7000, 10000},
		}, []string{"stage"}),
		TaskStepLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_step_latency_ms",
			Help:      "Task step execution latency in milliseconds.",
			Buckets:   []float64{50, 100, 200, 400, 700, 1200, 2000, 4000, 7000, 15000, 30000},
		}),
		TaskApprovalWait: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_approval_wait_ms",
			Help:      "Time spent waiting for user approval before execution.",
			Buckets:   []float64{100, 250, 500, 1000, 2000, 5000, 10000, 30000, 60000, 120000},
		}),
		turnStageWindow: newTurnStageWindow(256),
	}
}

func (m *Metrics) ObserveFirstAudioLatency(d time.Duration) {
	m.FirstAudioLatency.Observe(float64(d.Milliseconds()))
}

func (m *Metrics) ObserveTurnStage(stage string, d time.Duration) {
	ms := float64(d.Milliseconds())
	m.TurnStageLatency.WithLabelValues(stage).Observe(ms)
	m.turnStageWindow.Observe(stage, ms)
}

func (m *Metrics) ObserveOutboundMessage(msgType, result string) {
	m.OutboundMessages.WithLabelValues(msgType, result).Inc()
}

func (m *Metrics) ObserveTaskEvent(event string) {
	if m == nil || m.TaskEvents == nil {
		return
	}
	m.TaskEvents.WithLabelValues(event).Inc()
}

func (m *Metrics) ObserveTaskStepLatency(d time.Duration) {
	if m == nil || m.TaskStepLatency == nil {
		return
	}
	m.TaskStepLatency.Observe(float64(d.Milliseconds()))
}

func (m *Metrics) ObserveTaskApprovalWait(d time.Duration) {
	if m == nil || m.TaskApprovalWait == nil {
		return
	}
	m.TaskApprovalWait.Observe(float64(d.Milliseconds()))
}

func (m *Metrics) SnapshotTurnStages() TurnStageSnapshot {
	if m.turnStageWindow == nil {
		return TurnStageSnapshot{}
	}
	return m.turnStageWindow.Snapshot()
}

func (m *Metrics) ResetTurnStages() {
	if m == nil || m.turnStageWindow == nil {
		return
	}
	m.turnStageWindow.Reset()
}

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
