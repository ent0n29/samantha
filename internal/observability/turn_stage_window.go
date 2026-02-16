package observability

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type TurnStageStats struct {
	Stage       string  `json:"stage"`
	Samples     int     `json:"samples"`
	LastMS      float64 `json:"last_ms"`
	AvgMS       float64 `json:"avg_ms"`
	P50MS       float64 `json:"p50_ms"`
	P95MS       float64 `json:"p95_ms"`
	P99MS       float64 `json:"p99_ms"`
	TargetP95MS float64 `json:"target_p95_ms,omitempty"`
}

type TurnIndicator struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type TurnStageSnapshot struct {
	GeneratedAt time.Time        `json:"generated_at"`
	WindowSize  int              `json:"window_size"`
	Stages      []TurnStageStats `json:"stages"`
	Indicators  []TurnIndicator  `json:"indicators,omitempty"`
}

type turnStageWindow struct {
	mu         sync.RWMutex
	maxSamples int
	stages     map[string]*turnStageBuffer
	indicators map[string]int
}

type turnStageBuffer struct {
	values []float64
	next   int
	filled bool
	last   float64
}

func newTurnStageWindow(maxSamples int) *turnStageWindow {
	if maxSamples <= 0 {
		maxSamples = 256
	}
	return &turnStageWindow{
		maxSamples: maxSamples,
		stages:     make(map[string]*turnStageBuffer),
		indicators: make(map[string]int),
	}
}

func (w *turnStageWindow) Observe(stage string, ms float64) {
	if stage == "" || ms < 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	buf, ok := w.stages[stage]
	if !ok {
		buf = &turnStageBuffer{
			values: make([]float64, w.maxSamples),
		}
		w.stages[stage] = buf
	}
	buf.values[buf.next] = ms
	buf.last = ms
	buf.next++
	if buf.next >= len(buf.values) {
		buf.next = 0
		buf.filled = true
	}
}

func (w *turnStageWindow) Snapshot() TurnStageSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	stages := make([]TurnStageStats, 0, len(w.stages))
	keys := make([]string, 0, len(w.stages))
	for stage := range w.stages {
		keys = append(keys, stage)
	}
	sort.Strings(keys)

	for _, stage := range keys {
		buf := w.stages[stage]
		if buf == nil {
			continue
		}
		n := buf.next
		if buf.filled {
			n = len(buf.values)
		}
		if n <= 0 {
			continue
		}
		samples := make([]float64, n)
		copy(samples, buf.values[:n])
		sort.Float64s(samples)

		sum := 0.0
		for _, v := range samples {
			sum += v
		}

		stages = append(stages, TurnStageStats{
			Stage:       stage,
			Samples:     n,
			LastMS:      round2(buf.last),
			AvgMS:       round2(sum / float64(n)),
			P50MS:       round2(quantile(samples, 0.50)),
			P95MS:       round2(quantile(samples, 0.95)),
			P99MS:       round2(quantile(samples, 0.99)),
			TargetP95MS: stageTargetP95MS(stage),
		})
	}

	indicators := make([]TurnIndicator, 0, len(w.indicators))
	indicatorKeys := make([]string, 0, len(w.indicators))
	for name := range w.indicators {
		indicatorKeys = append(indicatorKeys, name)
	}
	sort.Strings(indicatorKeys)
	for _, name := range indicatorKeys {
		count := w.indicators[name]
		if count <= 0 {
			continue
		}
		indicators = append(indicators, TurnIndicator{
			Name:  name,
			Count: count,
		})
	}

	return TurnStageSnapshot{
		GeneratedAt: time.Now().UTC(),
		WindowSize:  w.maxSamples,
		Stages:      stages,
		Indicators:  indicators,
	}
}

func (w *turnStageWindow) ObserveIndicator(name string) {
	if w == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.indicators[name]++
}

func (w *turnStageWindow) Reset() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stages = make(map[string]*turnStageBuffer)
	w.indicators = make(map[string]int)
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := q * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func stageTargetP95MS(stage string) float64 {
	switch stage {
	case "commit_to_tts_ready":
		return 250
	case "commit_to_context_ready":
		return 350
	case "commit_to_assistant_working":
		return 650
	case "commit_to_thinking_delta":
		return 900
	case "commit_to_first_text":
		return 550
	case "commit_to_first_audio":
		return 1400
	case "turn_total":
		return 3200
	default:
		return 0
	}
}
