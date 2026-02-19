package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/ent0n29/samantha/internal/memory"
	"github.com/ent0n29/samantha/internal/observability"
	"github.com/ent0n29/samantha/internal/openclaw"
	"github.com/ent0n29/samantha/internal/policy"
	"github.com/ent0n29/samantha/internal/protocol"
	"github.com/ent0n29/samantha/internal/reliability"
	"github.com/ent0n29/samantha/internal/session"
	"github.com/ent0n29/samantha/internal/taskruntime"
	"github.com/ent0n29/samantha/internal/tasks"
)

type PersonaProfile struct {
	ID           string
	DisplayName  string
	SystemStyle  string
	VoiceID      string
	ModelID      string
	SpeakingRate float64
	Warmth       float64
	VerbosityCap int
}

type brainPrefetchResult struct {
	canonicalInput string
	deltas         []string
	finalText      string
}

type Orchestrator struct {
	sessions              *session.Manager
	adapter               openclaw.Adapter
	memoryStore           memory.Store
	sttProvider           STTProvider
	ttsProvider           TTSProvider
	metrics               *observability.Metrics
	firstAudioSLO         time.Duration
	assistantWorkingDelay time.Duration
	defaultVoice          string
	defaultModel          string
	sttLabel              string
	ttsLabel              string
	profiles              map[string]PersonaProfile
	strictOutbound        bool
	outboundMode          string
	taskService           *taskruntime.Service
}

type brainPrewarmCapable interface {
	PrewarmSession(ctx context.Context, sessionID string) error
}

const (
	memoryContextLimit            = 8
	memoryContextTimeout          = 350 * time.Millisecond
	memoryContextSoftWait         = 120 * time.Millisecond
	memoryContextPrefetchFresh    = 2 * time.Second
	brainPrefetchFresh            = 3 * time.Second
	brainPrefetchWaitBudget       = 280 * time.Millisecond
	brainPrefetchWaitMatureAfter  = 220 * time.Millisecond
	brainPrefetchWaitBudgetMature = 1600 * time.Millisecond
	brainPrefetchShortMaxWords    = 8
	brainPrefetchWaitBudgetShort  = 900 * time.Millisecond
	brainPrefetchWaitBudgetShortM = 2400 * time.Millisecond
	brainPrefetchMinCanonical     = 4
	brainPrefetchMinWords         = 2
	brainPrefetchStableRepeats    = 1
	brainPrefetchDebounce         = 260 * time.Millisecond
	brainPrefetchEarlyMinWords    = 2
	brainPrefetchEarlyAge         = 1200 * time.Millisecond
	brainPrefetchMemoryCtxLimit   = 3
	brainPrefetchCacheFresh       = 90 * time.Second
	brainPrefetchCacheMaxEntries  = 24
	brainFirstDeltaRetryTimeout   = 1400 * time.Millisecond
	brainFirstDeltaRetryMax       = 1
	brainWarmupTimeout            = 1800 * time.Millisecond
	wakeWordWindow                = 30 * time.Second
	memorySaveTimeout             = 2 * time.Second
	ttsFinalizeTimeout            = 10 * time.Second
	thinkingDeltaPreviewMaxRunes  = 92
)

var errBrainFirstDeltaTimeout = errors.New("brain first delta timeout")

func NewOrchestrator(
	sessions *session.Manager,
	adapter openclaw.Adapter,
	memoryStore memory.Store,
	sttProvider STTProvider,
	ttsProvider TTSProvider,
	metrics *observability.Metrics,
	firstAudioSLO time.Duration,
	assistantWorkingDelay time.Duration,
	defaultVoice string,
	defaultModel string,
	voiceProvider string,
	strictOutbound bool,
	outboundMode string,
	taskService *taskruntime.Service,
) *Orchestrator {
	vp := strings.ToLower(strings.TrimSpace(voiceProvider))
	if vp == "" {
		vp = "unknown"
	}
	profiles := map[string]PersonaProfile{
		"warm": {
			ID:           "warm",
			DisplayName:  "Warm",
			SystemStyle:  "empathetic, conversational, supportive",
			VoiceID:      defaultVoice,
			ModelID:      defaultModel,
			SpeakingRate: 0.9,
			Warmth:       0.9,
			VerbosityCap: 350,
		},
		"professional": {
			ID:           "professional",
			DisplayName:  "Professional",
			SystemStyle:  "clear, factual, concise",
			VoiceID:      defaultVoice,
			ModelID:      defaultModel,
			SpeakingRate: 0.92,
			Warmth:       0.4,
			VerbosityCap: 280,
		},
		"concise": {
			ID:           "concise",
			DisplayName:  "Concise",
			SystemStyle:  "brief, high-signal, direct",
			VoiceID:      defaultVoice,
			ModelID:      defaultModel,
			SpeakingRate: 0.97,
			Warmth:       0.3,
			VerbosityCap: 180,
		},
	}

	mode := strings.ToLower(strings.TrimSpace(outboundMode))
	if mode == "" {
		mode = "drop"
	}
	if mode != "drop" && mode != "block" {
		mode = "drop"
	}

	return &Orchestrator{
		sessions:              sessions,
		adapter:               adapter,
		memoryStore:           memoryStore,
		sttProvider:           sttProvider,
		ttsProvider:           ttsProvider,
		metrics:               metrics,
		firstAudioSLO:         firstAudioSLO,
		assistantWorkingDelay: assistantWorkingDelay,
		defaultVoice:          defaultVoice,
		defaultModel:          defaultModel,
		sttLabel:              vp + "_stt",
		ttsLabel:              vp + "_tts",
		profiles:              profiles,
		strictOutbound:        strictOutbound,
		outboundMode:          mode,
		taskService:           taskService,
	}
}

// PreviewTTS synthesizes a short standalone utterance for voice auditioning.
// This bypasses OpenClaw and memory; it only uses the configured TTS provider.
func (o *Orchestrator) PreviewTTS(ctx context.Context, voiceID, modelID, personaID, text string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	profile := o.profileForPersona(personaID)
	if strings.TrimSpace(voiceID) != "" {
		profile.VoiceID = strings.TrimSpace(voiceID)
	}
	if strings.TrimSpace(modelID) != "" {
		profile.ModelID = strings.TrimSpace(modelID)
	}

	if strings.TrimSpace(profile.VoiceID) == "" {
		return nil, "", fmt.Errorf("voice_id is required")
	}
	if strings.TrimSpace(profile.ModelID) == "" {
		profile.ModelID = o.defaultModel
	}
	if strings.TrimSpace(text) == "" {
		text = "Hi. I'm here with you."
	}

	settings := ttsSettingsForProfile(profile)
	stream, err := o.ttsProvider.StartStream(ctx, profile.VoiceID, profile.ModelID, settings)
	if err != nil {
		return nil, "", err
	}
	defer stream.Close()

	if err := stream.SendText(ctx, text, true); err != nil {
		_ = stream.CloseInput(ctx)
		return nil, "", err
	}
	_ = stream.CloseInput(ctx)

	var out bytes.Buffer
	var format string
	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case evt, ok := <-stream.Events():
			if !ok {
				return out.Bytes(), strings.TrimSpace(format), nil
			}
			switch evt.Type {
			case TTSEventAudio:
				if format == "" && strings.TrimSpace(evt.Format) != "" {
					format = strings.TrimSpace(evt.Format)
				}
				if strings.TrimSpace(evt.AudioBase64) == "" {
					continue
				}
				chunk, err := base64.StdEncoding.DecodeString(evt.AudioBase64)
				if err != nil {
					return nil, "", fmt.Errorf("decode audio chunk: %w", err)
				}
				_, _ = out.Write(chunk)
			case TTSEventFinal:
				return out.Bytes(), strings.TrimSpace(format), nil
			case TTSEventError:
				return nil, "", fmt.Errorf("tts error: %s %s", strings.TrimSpace(evt.Code), strings.TrimSpace(evt.Detail))
			}
		}
	}
}

// RunConnection drives a session lifecycle for one websocket connection.
func (o *Orchestrator) RunConnection(ctx context.Context, s *session.Session, inbound <-chan any, outbound chan<- any) error {
	sttSession, sttEvents, err := o.sttProvider.StartSession(ctx, s.ID)
	if err != nil {
		o.send(outbound, protocol.ErrorEvent{
			Type:      protocol.TypeErrorEvent,
			SessionID: s.ID,
			Code:      "stt_connect_failed",
			Source:    "stt",
			Retryable: reliability.IsRetryableHTTPStatus(503),
			Detail:    err.Error(),
		})
		return err
	}
	defer sttSession.Close()

	var (
		taskEvents <-chan tasks.Event
		taskUnsub  func()
	)
	if o.taskService != nil && o.taskService.Enabled() {
		taskEvents, taskUnsub = o.taskService.Subscribe(s.ID)
		o.send(outbound, o.taskStatusSnapshotForSession(s.ID))
	}
	if taskUnsub != nil {
		defer taskUnsub()
	}

	var (
		turnMu                   sync.Mutex
		turnCancel               context.CancelFunc
		activeTurnID             string
		activeToken              int64
		nextToken                int64
		lastSampleHz             = 16000
		lastStopAt               time.Time
		assistantOutputStartedAt atomic.Int64

		wakewordEnabled    bool
		manualArmUntil     time.Time
		awaitingQueryUntil time.Time

		memPrefetchMu       sync.Mutex
		memPrefetchRecent   []memory.TurnRecord
		memPrefetchReadyAt  time.Time
		memPrefetchInFlight bool

		brainPrefetchMu        sync.Mutex
		brainPrefetchGen       int64
		brainPrefetchInFlight  bool
		brainPrefetchCancel    context.CancelFunc
		brainPrefetchCanonical string
		brainPrefetchDone      chan struct{}
		brainPrefetchResultVal *brainPrefetchResult
		brainPrefetchReadyAt   time.Time
		brainPrefetchStartedAt time.Time
		brainPrefetchCache     = map[string]brainPrefetchResult{}
		brainPrefetchCacheAt   = map[string]time.Time{}
		brainPrefetchCacheLRU  []string

		partialStableCanonical string
		partialStableRepeats   int
		lastBrainPrefetchAt    time.Time
		utteranceStartedAt     time.Time
		lastPartialAt          time.Time
		semanticHintState      semanticEndpointDispatchState
	)

	o.startBrainSessionWarmup(ctx, s.ID)

	startMemoryPrefetch := func() {
		if o.memoryStore == nil {
			return
		}
		memPrefetchMu.Lock()
		if memPrefetchInFlight {
			memPrefetchMu.Unlock()
			return
		}
		if !memPrefetchReadyAt.IsZero() && time.Since(memPrefetchReadyAt) < memoryContextPrefetchFresh/2 {
			memPrefetchMu.Unlock()
			return
		}
		memPrefetchInFlight = true
		memPrefetchMu.Unlock()

		go func(userID string) {
			recentCtx, cancel := context.WithTimeout(ctx, memoryContextTimeout)
			defer cancel()
			recent, err := o.memoryStore.RecentContext(recentCtx, userID, memoryContextLimit)
			now := time.Now()

			memPrefetchMu.Lock()
			defer memPrefetchMu.Unlock()
			memPrefetchInFlight = false
			if err != nil {
				return
			}
			memPrefetchRecent = append(memPrefetchRecent[:0], recent...)
			memPrefetchReadyAt = now
		}(s.UserID)
	}

	getMemoryPrefetch := func() ([]memory.TurnRecord, time.Time, bool) {
		if o.memoryStore == nil {
			return nil, time.Time{}, false
		}
		memPrefetchMu.Lock()
		defer memPrefetchMu.Unlock()
		if memPrefetchReadyAt.IsZero() {
			return nil, time.Time{}, false
		}
		if time.Since(memPrefetchReadyAt) > memoryContextPrefetchFresh {
			return nil, time.Time{}, false
		}
		out := make([]memory.TurnRecord, len(memPrefetchRecent))
		copy(out, memPrefetchRecent)
		return out, memPrefetchReadyAt, true
	}

	cancelBrainPrefetch := func() {
		brainPrefetchMu.Lock()
		cancel := brainPrefetchCancel
		brainPrefetchInFlight = false
		brainPrefetchCancel = nil
		brainPrefetchCanonical = ""
		brainPrefetchDone = nil
		brainPrefetchResultVal = nil
		brainPrefetchReadyAt = time.Time{}
		brainPrefetchStartedAt = time.Time{}
		brainPrefetchMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	storeBrainPrefetchCache := func(ready *brainPrefetchResult) {
		if ready == nil {
			return
		}
		canonical := strings.TrimSpace(ready.canonicalInput)
		if canonical == "" {
			return
		}

		brainPrefetchMu.Lock()
		defer brainPrefetchMu.Unlock()

		clone := brainPrefetchResult{
			canonicalInput: canonical,
			deltas:         append([]string(nil), ready.deltas...),
			finalText:      ready.finalText,
		}
		brainPrefetchCache[canonical] = clone
		brainPrefetchCacheAt[canonical] = time.Now()

		for i, key := range brainPrefetchCacheLRU {
			if key == canonical {
				brainPrefetchCacheLRU = append(brainPrefetchCacheLRU[:i], brainPrefetchCacheLRU[i+1:]...)
				break
			}
		}
		brainPrefetchCacheLRU = append(brainPrefetchCacheLRU, canonical)

		for len(brainPrefetchCacheLRU) > brainPrefetchCacheMaxEntries {
			evict := brainPrefetchCacheLRU[0]
			brainPrefetchCacheLRU = brainPrefetchCacheLRU[1:]
			delete(brainPrefetchCache, evict)
			delete(brainPrefetchCacheAt, evict)
		}
		o.metrics.SessionEvents.WithLabelValues("brain_prefetch_cache_store").Inc()
	}

	tryConsumeBrainPrefetchCache := func(canonical string) *brainPrefetchResult {
		canonical = strings.TrimSpace(canonical)
		if canonical == "" {
			return nil
		}

		brainPrefetchMu.Lock()
		defer brainPrefetchMu.Unlock()

		bestKey := ""
		bestScore := -1
		for key, readyAt := range brainPrefetchCacheAt {
			if readyAt.IsZero() || time.Since(readyAt) > brainPrefetchCacheFresh {
				delete(brainPrefetchCache, key)
				delete(brainPrefetchCacheAt, key)
				for i, lruKey := range brainPrefetchCacheLRU {
					if lruKey == key {
						brainPrefetchCacheLRU = append(brainPrefetchCacheLRU[:i], brainPrefetchCacheLRU[i+1:]...)
						break
					}
				}
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_cache_stale").Inc()
				continue
			}
			if key == canonical {
				bestKey = key
				bestScore = 1 << 20
				break
			}
			if !brainPrefetchCanonicalCompatible(key, canonical) {
				continue
			}
			score := sharedWordPrefixCount(strings.Fields(key), strings.Fields(canonical))
			if score > bestScore {
				bestScore = score
				bestKey = key
			}
		}
		if bestKey == "" {
			return nil
		}
		entry, ok := brainPrefetchCache[bestKey]
		if !ok {
			return nil
		}
		for i, key := range brainPrefetchCacheLRU {
			if key == bestKey {
				brainPrefetchCacheLRU = append(brainPrefetchCacheLRU[:i], brainPrefetchCacheLRU[i+1:]...)
				break
			}
		}
		brainPrefetchCacheLRU = append(brainPrefetchCacheLRU, bestKey)
		if bestKey == canonical {
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_cache_hit").Inc()
		} else {
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_cache_fuzzy_hit").Inc()
		}
		return &brainPrefetchResult{
			canonicalInput: entry.canonicalInput,
			deltas:         append([]string(nil), entry.deltas...),
			finalText:      entry.finalText,
		}
	}

	tryConsumeBrainPrefetch := func(canonical string) *brainPrefetchResult {
		canonical = strings.TrimSpace(canonical)
		if canonical == "" {
			return nil
		}

		getReady := func() *brainPrefetchResult {
			brainPrefetchMu.Lock()
			defer brainPrefetchMu.Unlock()
			if brainPrefetchResultVal == nil {
				return nil
			}
			if !brainPrefetchCanonicalCompatible(brainPrefetchResultVal.canonicalInput, canonical) {
				return nil
			}
			if brainPrefetchReadyAt.IsZero() || time.Since(brainPrefetchReadyAt) > brainPrefetchFresh {
				return nil
			}
			wasExactMatch := brainPrefetchResultVal.canonicalInput == canonical
			out := &brainPrefetchResult{
				canonicalInput: brainPrefetchResultVal.canonicalInput,
				deltas:         append([]string(nil), brainPrefetchResultVal.deltas...),
				finalText:      brainPrefetchResultVal.finalText,
			}
			brainPrefetchResultVal = nil
			brainPrefetchReadyAt = time.Time{}
			if !wasExactMatch {
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_fuzzy_hit").Inc()
			} else {
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_hit").Inc()
			}
			return out
		}
		if ready := getReady(); ready != nil {
			return ready
		}
		if cached := tryConsumeBrainPrefetchCache(canonical); cached != nil {
			return cached
		}

		var (
			done      chan struct{}
			startedAt time.Time
		)
		brainPrefetchMu.Lock()
		if brainPrefetchInFlight && brainPrefetchCanonicalCompatible(brainPrefetchCanonical, canonical) {
			done = brainPrefetchDone
			startedAt = brainPrefetchStartedAt
		}
		brainPrefetchMu.Unlock()
		if done != nil {
			canonicalWords := wordsInCanonical(canonical)
			shortCanonical := canonicalWords > 0 && canonicalWords <= brainPrefetchShortMaxWords
			waitBudget := brainPrefetchWaitBudget
			mature := !startedAt.IsZero() && time.Since(startedAt) >= brainPrefetchWaitMatureAfter
			if shortCanonical {
				waitBudget = brainPrefetchWaitBudgetShort
				if mature {
					waitBudget = brainPrefetchWaitBudgetShortM
					o.metrics.SessionEvents.WithLabelValues("brain_prefetch_wait_short_mature").Inc()
				} else {
					o.metrics.SessionEvents.WithLabelValues("brain_prefetch_wait_short").Inc()
				}
			} else if mature {
				waitBudget = brainPrefetchWaitBudgetMature
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_wait_mature").Inc()
			}
			timer := time.NewTimer(waitBudget)
			select {
			case <-done:
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_wait_hit").Inc()
			case <-timer.C:
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_wait_timeout").Inc()
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if ready := getReady(); ready != nil {
			return ready
		}
		return tryConsumeBrainPrefetchCache(canonical)
	}

	startBrainPrefetch := func(rawText string, canonical string) {
		rawText = strings.TrimSpace(rawText)
		canonical = strings.TrimSpace(canonical)
		if rawText == "" || !shouldSpeculateBrainCanonical(canonical) {
			return
		}
		o.metrics.SessionEvents.WithLabelValues("brain_prefetch_start_attempt").Inc()

		brainPrefetchMu.Lock()
		if brainPrefetchInFlight {
			if shouldKeepBrainPrefetchInFlight(brainPrefetchCanonical, canonical) {
				brainPrefetchMu.Unlock()
				o.metrics.SessionEvents.WithLabelValues("brain_prefetch_kept_inflight").Inc()
				return
			}
		}
		if brainPrefetchResultVal != nil &&
			brainPrefetchCanonicalCompatible(brainPrefetchResultVal.canonicalInput, canonical) &&
			!brainPrefetchReadyAt.IsZero() &&
			time.Since(brainPrefetchReadyAt) < brainPrefetchFresh {
			brainPrefetchMu.Unlock()
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_ready_reuse").Inc()
			return
		}

		oldCancel := brainPrefetchCancel
		brainPrefetchGen++
		gen := brainPrefetchGen
		specCtx, specCancel := context.WithCancel(ctx)
		doneCh := make(chan struct{})
		brainPrefetchInFlight = true
		brainPrefetchCancel = specCancel
		brainPrefetchCanonical = canonical
		brainPrefetchDone = doneCh
		brainPrefetchResultVal = nil
		brainPrefetchReadyAt = time.Time{}
		brainPrefetchStartedAt = time.Now()
		brainPrefetchMu.Unlock()

		if oldCancel != nil {
			oldCancel()
		}
		o.metrics.SessionEvents.WithLabelValues("brain_prefetch_started").Inc()

		recent, _, _ := getMemoryPrefetch()
		contextLines := make([]string, 0, len(recent))
		for _, r := range recent {
			contextLines = append(contextLines, fmt.Sprintf("%s: %s", r.Role, r.Content))
		}
		if len(contextLines) > brainPrefetchMemoryCtxLimit {
			contextLines = contextLines[len(contextLines)-brainPrefetchMemoryCtxLimit:]
		}
		personaID := o.profileForSession(*s).ID
		prefetchAdapter := o.adapter
		if fb, ok := o.adapter.(*openclaw.FallbackAdapter); ok {
			if primary := fb.Primary(); primary != nil {
				prefetchAdapter = primary
			}
		}

		go func(ctx context.Context, gen int64, canonical, inputText string, done chan struct{}, memoryContext []string, personaID string, adapter openclaw.Adapter) {
			defer close(done)
			deltas := make([]string, 0, 24)
			resp, err := adapter.StreamResponse(ctx, openclaw.MessageRequest{
				UserID:        s.UserID,
				SessionID:     s.ID,
				TurnID:        "spec-" + uuid.NewString(),
				InputText:     inputText,
				MemoryContext: memoryContext,
				PersonaID:     personaID,
			}, func(delta string) error {
				if strings.TrimSpace(delta) != "" {
					deltas = append(deltas, delta)
				}
				return nil
			})

			ready := &brainPrefetchResult{
				canonicalInput: canonical,
				deltas:         deltas,
				finalText:      strings.TrimSpace(resp.Text),
			}

			var cacheReady *brainPrefetchResult
			brainPrefetchMu.Lock()
			if brainPrefetchGen != gen || brainPrefetchCanonical != canonical {
				brainPrefetchMu.Unlock()
				return
			}
			brainPrefetchInFlight = false
			brainPrefetchCancel = nil
			if err != nil {
				brainPrefetchMu.Unlock()
				return
			}
			if len(ready.deltas) == 0 && ready.finalText == "" {
				brainPrefetchMu.Unlock()
				return
			}
			brainPrefetchResultVal = ready
			brainPrefetchReadyAt = time.Now()
			cacheReady = &brainPrefetchResult{
				canonicalInput: ready.canonicalInput,
				deltas:         append([]string(nil), ready.deltas...),
				finalText:      ready.finalText,
			}
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_ready").Inc()
			brainPrefetchMu.Unlock()
			storeBrainPrefetchCache(cacheReady)
		}(specCtx, gen, canonical, rawText, doneCh, contextLines, personaID, prefetchAdapter)
	}

	maybeStartBrainPrefetch := func(specText string, now time.Time, utteranceAge time.Duration) {
		canonical := canonicalizeBrainPrefetchInput(specText)
		if canonical == "" {
			return
		}
		if !shouldSpeculateBrainCanonical(canonical) {
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_skip_short").Inc()
			return
		}
		earlyStart := shouldStartBrainPrefetchEarly(specText, canonical, utteranceAge)
		if canonical == partialStableCanonical {
			partialStableRepeats++
		} else if canonicalIsProgressiveContinuation(partialStableCanonical, canonical) {
			partialStableCanonical = canonical
			partialStableRepeats++
		} else {
			partialStableCanonical = canonical
			partialStableRepeats = 1
		}
		requiredRepeats := brainPrefetchStableRepeats
		if earlyStart {
			requiredRepeats = 1
		}
		if partialStableRepeats < requiredRepeats {
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_skip_unstable").Inc()
			return
		}
		if !lastBrainPrefetchAt.IsZero() && now.Sub(lastBrainPrefetchAt) < brainPrefetchDebounce {
			o.metrics.SessionEvents.WithLabelValues("brain_prefetch_skip_debounce").Inc()
			return
		}
		lastBrainPrefetchAt = now
		startBrainPrefetch(specText, canonical)
	}

	startMemoryPrefetch()

	cancelActiveTurn := func(reason string) {
		turnMu.Lock()
		cancel := turnCancel
		turnID := activeTurnID
		turnCancel = nil
		activeTurnID = ""
		turnMu.Unlock()

		if cancel != nil {
			cancel()
			o.send(outbound, protocol.AssistantTurnEnd{
				Type:      protocol.TypeAssistantTurnEnd,
				SessionID: s.ID,
				TurnID:    turnID,
				Reason:    reason,
			})
		}
	}

	for {
		select {
		case <-ctx.Done():
			cancelBrainPrefetch()
			cancelActiveTurn("connection_closed")
			return nil
		case msg, ok := <-inbound:
			if !ok {
				cancelBrainPrefetch()
				cancelActiveTurn("connection_closed")
				return nil
			}
			switch m := msg.(type) {
			case protocol.ClientAudioChunk:
				if m.SampleRate > 0 {
					lastSampleHz = m.SampleRate
				}
				_ = o.sessions.Touch(s.ID)
				if err := sttSession.SendAudioChunk(ctx, m.PCM16Base64, m.SampleRate, false); err != nil {
					o.send(outbound, protocol.ErrorEvent{
						Type:      protocol.TypeErrorEvent,
						SessionID: s.ID,
						Code:      "stt_send_audio_failed",
						Source:    "stt",
						Retryable: true,
						Detail:    err.Error(),
					})
				}
			case protocol.ClientControl:
				_ = o.sessions.Touch(s.ID)
				switch m.Action {
				case "interrupt":
					reason := normalizeControlReason(m.Reason)
					if reason == "" {
						reason = "unknown"
					}
					o.metrics.ObserveTurnIndicator("interrupt_reason_" + reason)
					if reason == "barge_interrupt" {
						startedAtMs := assistantOutputStartedAt.Load()
						if startedAtMs > 0 {
							if d := time.Since(time.UnixMilli(startedAtMs)); d >= 0 && d <= 500*time.Millisecond {
								o.metrics.ObserveTurnIndicator("cutoff_suspected")
							}
						}
					}
					_ = o.sessions.Interrupt(s.ID)
					cancelActiveTurn("interrupted")
				case "stop":
					reason := normalizeControlReason(m.Reason)
					if reason == "" {
						reason = "unknown"
					}
					o.metrics.ObserveTurnIndicator("auto_commit_reason_" + reason)
					lastStopAt = time.Now()
					_ = sttSession.SendAudioChunk(ctx, "", lastSampleHz, true)
				case "approve_task_step":
					if o.taskService != nil && o.taskService.Enabled() {
						approved := true
						if m.Approved != nil {
							approved = *m.Approved
						}
						var err error
						if strings.TrimSpace(m.TaskID) != "" {
							_, err = o.taskService.ApproveTask(ctx, m.TaskID, approved)
						} else {
							_, err = o.taskService.ApproveLatestForSession(ctx, s.ID, approved)
						}
						if err != nil {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_approval_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
					}
				case "deny_task_step":
					if o.taskService != nil && o.taskService.Enabled() {
						var err error
						if strings.TrimSpace(m.TaskID) != "" {
							_, err = o.taskService.ApproveTask(ctx, m.TaskID, false)
						} else {
							_, err = o.taskService.ApproveLatestForSession(ctx, s.ID, false)
						}
						if err != nil {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_deny_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
					}
				case "cancel_task":
					if o.taskService != nil && o.taskService.Enabled() {
						var err error
						if strings.TrimSpace(m.TaskID) != "" {
							_, err = o.taskService.CancelTask(ctx, m.TaskID, "Cancelled by user.")
						} else {
							_, err = o.taskService.CancelActiveForSession(ctx, s.ID, "Cancelled by user.")
						}
						if err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_cancel_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
					}
				case "pause_task":
					if o.taskService != nil && o.taskService.Enabled() {
						var err error
						if strings.TrimSpace(m.TaskID) != "" {
							_, err = o.taskService.PauseTask(ctx, m.TaskID, "Paused by user.")
						} else {
							_, err = o.taskService.PauseActiveForSession(ctx, s.ID, "Paused by user.")
						}
						if err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_pause_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
					}
				case "resume_task":
					if o.taskService != nil && o.taskService.Enabled() {
						var err error
						if strings.TrimSpace(m.TaskID) != "" {
							_, err = o.taskService.ResumeTask(ctx, m.TaskID)
						} else {
							_, err = o.taskService.ResumeLatestForSession(ctx, s.ID)
						}
						if err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_resume_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
					}
				case "wakeword_on":
					wakewordEnabled = true
					manualArmUntil = time.Time{}
					awaitingQueryUntil = time.Time{}
				case "wakeword_off":
					wakewordEnabled = false
					manualArmUntil = time.Time{}
					awaitingQueryUntil = time.Time{}
				case "manual_arm":
					manualArmUntil = time.Now().Add(12 * time.Second)
					awaitingQueryUntil = time.Time{}
				case "start", "mute", "unmute":
					// no-op currently
				}
			}
		case taskEvt, ok := <-taskEvents:
			if !ok {
				taskEvents = nil
				continue
			}
			switch taskEvt.Type {
			case tasks.EventTaskCompleted:
				note := strings.TrimSpace(taskEvt.Result)
				if note == "" {
					note = fmt.Sprintf("Task %s completed.", taskEvt.TaskID)
				} else {
					note = fmt.Sprintf("Task %s completed: %s", taskEvt.TaskID, note)
				}
				redacted, changed := policy.RedactPII(note)
				o.saveTurnBestEffort(memory.TurnRecord{
					ID:          uuid.NewString(),
					UserID:      s.UserID,
					SessionID:   s.ID,
					Role:        "assistant",
					Content:     redacted,
					PIIRedacted: changed,
					CreatedAt:   time.Now().UTC(),
				})
			case tasks.EventTaskFailed:
				note := strings.TrimSpace(taskEvt.Detail)
				if note == "" {
					note = fmt.Sprintf("Task %s failed.", taskEvt.TaskID)
				} else {
					note = fmt.Sprintf("Task %s failed: %s", taskEvt.TaskID, note)
				}
				redacted, changed := policy.RedactPII(note)
				o.saveTurnBestEffort(memory.TurnRecord{
					ID:          uuid.NewString(),
					UserID:      s.UserID,
					SessionID:   s.ID,
					Role:        "assistant",
					Content:     redacted,
					PIIRedacted: changed,
					CreatedAt:   time.Now().UTC(),
				})
			}
			o.sendTaskEvent(outbound, taskEvt)
		case evt, ok := <-sttEvents:
			if !ok {
				cancelBrainPrefetch()
				cancelActiveTurn("stt_closed")
				return nil
			}

			switch evt.Type {
			case STTEventPartial:
				now := time.Now()
				partialText := strings.TrimSpace(evt.Text)
				if partialText != "" {
					o.metrics.SessionEvents.WithLabelValues("stt_partial_seen").Inc()
					lastPartialAt = now
				}
				if partialText != "" && utteranceStartedAt.IsZero() {
					utteranceStartedAt = now
				}
				if partialText != "" {
					if hint, ok := buildSemanticEndpointHint(partialText, evt.Confidence, now.Sub(utteranceStartedAt)); ok {
						reason := strings.TrimSpace(strings.ToLower(hint.Reason))
						if reason == "" {
							reason = "neutral"
						}
						if semanticHintState.ShouldEmit(hint, now) {
							o.metrics.ObserveTurnIndicator("semantic_hint_" + reason)
							o.send(outbound, protocol.SemanticEndpointHint{
								Type:         protocol.TypeSemanticEndpointHint,
								SessionID:    s.ID,
								Reason:       reason,
								Confidence:   hint.Confidence,
								HoldMS:       hint.Hold.Milliseconds(),
								ShouldCommit: hint.ShouldCommit,
								TSMs:         evt.Timestamp,
							})
						}
					}
				}
				specText := partialText
				if partialText != "" {
					startMemoryPrefetch()
					if wakewordEnabled {
						now := time.Now()
						manualArmed := !manualArmUntil.IsZero() && now.Before(manualArmUntil)
						awaitingQuery := !awaitingQueryUntil.IsZero() && now.Before(awaitingQueryUntil)

						// Don't speculate the brain while we're "asleep" in hands-free mode. We still emit
						// STT partials for debugging, but we avoid calling the brain until the wake word
						// (or manual arm) is present.
						hit, remainder := detectWakeWordCommitted(partialText)
						if hit {
							specText = remainder
						}
						if !manualArmed && !awaitingQuery && !hit {
							// Reset stability tracking so we don't accidentally carry state across unrelated
							// background speech while the wake word is off.
							partialStableCanonical = ""
							partialStableRepeats = 0
							lastBrainPrefetchAt = time.Time{}
						} else if strings.TrimSpace(specText) != "" {
							maybeStartBrainPrefetch(specText, now, now.Sub(utteranceStartedAt))
						}
					} else if strings.TrimSpace(specText) != "" {
						maybeStartBrainPrefetch(specText, now, now.Sub(utteranceStartedAt))
					}
				}
				o.send(outbound, protocol.STTPartial{
					Type:       protocol.TypeSTTPartial,
					SessionID:  s.ID,
					Text:       evt.Text,
					Confidence: evt.Confidence,
					TSMs:       evt.Timestamp,
				})
			case STTEventCommitted:
				committedText := strings.TrimSpace(evt.Text)
				if committedText == "" {
					continue
				}
				now := time.Now()
				if src := strings.TrimSpace(strings.ToLower(evt.Source)); src != "" {
					o.metrics.ObserveTurnIndicator("stt_commit_source_" + normalizeControlReason(src))
				}
				if !lastPartialAt.IsZero() && now.After(lastPartialAt) {
					o.metrics.ObserveTurnStage("partial_to_commit", now.Sub(lastPartialAt))
				}
				utteranceStartedAt = time.Time{}
				lastPartialAt = time.Time{}
				semanticHintState.Reset()
				if !lastStopAt.IsZero() {
					o.metrics.ObserveTurnStage("stop_to_stt_committed", time.Since(lastStopAt))
					lastStopAt = time.Time{}
				}
				partialStableCanonical = ""
				partialStableRepeats = 0
				lastBrainPrefetchAt = time.Time{}

				o.send(outbound, protocol.STTCommitted{
					Type:      protocol.TypeSTTCommitted,
					SessionID: s.ID,
					Text:      committedText,
					TSMs:      evt.Timestamp,
				})

				if !manualArmUntil.IsZero() && now.After(manualArmUntil) {
					manualArmUntil = time.Time{}
				}
				if !awaitingQueryUntil.IsZero() && now.After(awaitingQueryUntil) {
					awaitingQueryUntil = time.Time{}
				}

				if wakewordEnabled {
					// Hands-free: require wake word unless the user explicitly armed a push-to-talk fallback.
					manualArmed := !manualArmUntil.IsZero()
					awaitingQuery := !awaitingQueryUntil.IsZero()

					if awaitingQuery && !manualArmed {
						hit, remainder := detectWakeWordCommitted(committedText)
						if hit && strings.TrimSpace(remainder) == "" {
							// The user repeated the wake word; keep the window open.
							awaitingQueryUntil = now.Add(wakeWordWindow)
							o.send(outbound, protocol.SystemEvent{
								Type:      protocol.TypeSystemEvent,
								SessionID: s.ID,
								Code:      "wake_word",
								Detail:    "samantha",
							})
							cancelBrainPrefetch()
							continue
						}
						if hit && strings.TrimSpace(remainder) != "" {
							// Wake word + query in one breath: still notify the UI and keep the awake window open.
							o.send(outbound, protocol.SystemEvent{
								Type:      protocol.TypeSystemEvent,
								SessionID: s.ID,
								Code:      "wake_word",
								Detail:    "samantha",
							})
							committedText = remainder
						}
						// Accepted utterance while awake: keep the window open for follow-ups.
						awaitingQueryUntil = now.Add(wakeWordWindow)
					} else if manualArmed {
						manualArmUntil = time.Time{}
						hit, remainder := detectWakeWordCommitted(committedText)
						if hit {
							// Manual-arm counts as "awake", but if the user includes the wake word anyway,
							// strip it and keep the follow-up window open.
							o.send(outbound, protocol.SystemEvent{
								Type:      protocol.TypeSystemEvent,
								SessionID: s.ID,
								Code:      "wake_word",
								Detail:    "samantha",
							})
							if strings.TrimSpace(remainder) == "" {
								awaitingQueryUntil = now.Add(wakeWordWindow)
								cancelBrainPrefetch()
								continue
							}
							committedText = remainder
						}
						// After a manual-arm utterance, keep a short awake window for follow-ups.
						awaitingQueryUntil = now.Add(wakeWordWindow)
					} else {
						hit, remainder := detectWakeWordCommitted(committedText)
						if !hit {
							cancelBrainPrefetch()
							continue
						}
						// Wake word always opens (and refreshes) a short follow-up window so the user
						// can continue talking naturally without repeating it every turn.
						awaitingQueryUntil = now.Add(wakeWordWindow)
						o.send(outbound, protocol.SystemEvent{
							Type:      protocol.TypeSystemEvent,
							SessionID: s.ID,
							Code:      "wake_word",
							Detail:    "samantha",
						})
						if strings.TrimSpace(remainder) == "" {
							cancelBrainPrefetch()
							continue
						}
						committedText = remainder
					}

					committedText = strings.TrimSpace(committedText)
					if committedText == "" {
						cancelBrainPrefetch()
						continue
					}
				}

				if o.taskService != nil && o.taskService.Enabled() {
					lowerCommit := strings.ToLower(strings.TrimSpace(committedText))
					if lowerCommit == "approve task" || lowerCommit == "approve" {
						if _, err := o.taskService.ApproveLatestForSession(ctx, s.ID, true); err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_approval_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
						cancelBrainPrefetch()
						continue
					}
					if lowerCommit == "deny task" || lowerCommit == "deny" {
						if _, err := o.taskService.ApproveLatestForSession(ctx, s.ID, false); err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_deny_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
						cancelBrainPrefetch()
						continue
					}
					if lowerCommit == "cancel task" {
						if _, err := o.taskService.CancelActiveForSession(ctx, s.ID, "Cancelled by user."); err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_cancel_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
						cancelBrainPrefetch()
						continue
					}
					if lowerCommit == "pause task" {
						if _, err := o.taskService.PauseActiveForSession(ctx, s.ID, "Paused by user."); err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_pause_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
						cancelBrainPrefetch()
						continue
					}
					if lowerCommit == "resume task" || lowerCommit == "continue task" {
						if _, err := o.taskService.ResumeLatestForSession(ctx, s.ID); err != nil && !errors.Is(err, tasks.ErrTaskNotFound) {
							o.send(outbound, protocol.ErrorEvent{
								Type:      protocol.TypeErrorEvent,
								SessionID: s.ID,
								Code:      "task_resume_failed",
								Source:    "task_runtime",
								Retryable: false,
								Detail:    err.Error(),
							})
						}
						cancelBrainPrefetch()
						continue
					}

					createdTask, handledByTaskRuntime, err := o.taskService.MaybeCreateFromUtterance(ctx, s.ID, s.UserID, committedText)
					if err != nil {
						o.send(outbound, protocol.ErrorEvent{
							Type:      protocol.TypeErrorEvent,
							SessionID: s.ID,
							Code:      "task_create_failed",
							Source:    "task_runtime",
							Retryable: false,
							Detail:    err.Error(),
						})
					}
					if handledByTaskRuntime {
						redactedUserText, userChanged := policy.RedactPII(committedText)
						o.saveTurnBestEffort(memory.TurnRecord{
							ID:          uuid.NewString(),
							UserID:      s.UserID,
							SessionID:   s.ID,
							Role:        "user",
							Content:     redactedUserText,
							PIIRedacted: userChanged,
							CreatedAt:   time.Now().UTC(),
						})

						ackText := fmt.Sprintf("On it. I started task %s.", createdTask.ID)
						turnID := uuid.NewString()
						o.send(outbound, protocol.AssistantTextDelta{
							Type:      protocol.TypeAssistantTextDelta,
							SessionID: s.ID,
							TurnID:    turnID,
							TextDelta: ackText,
						})
						o.send(outbound, protocol.AssistantTurnEnd{
							Type:      protocol.TypeAssistantTurnEnd,
							SessionID: s.ID,
							TurnID:    turnID,
							Reason:    "completed",
						})
						redactedAck, ackChanged := policy.RedactPII(ackText)
						o.saveTurnBestEffort(memory.TurnRecord{
							ID:          uuid.NewString(),
							UserID:      s.UserID,
							SessionID:   s.ID,
							Role:        "assistant",
							Content:     redactedAck,
							PIIRedacted: ackChanged,
							CreatedAt:   time.Now().UTC(),
						})
						cancelBrainPrefetch()
						continue
					}
				}

				committedCanonical := canonicalizeBrainPrefetchInput(committedText)
				prefetchedBrain := tryConsumeBrainPrefetch(committedCanonical)
				cancelBrainPrefetch()

				// V0 is intentionally half-duplex: ignore fresh commits while assistant is talking.
				turnMu.Lock()
				busy := turnCancel != nil
				turnMu.Unlock()
				if busy {
					// User spoke while the assistant was still responding. Treat this as a barge-in:
					// cancel the current turn and pivot immediately.
					_ = o.sessions.Interrupt(s.ID)
					cancelActiveTurn("barge_in")
				}

				turnID := uuid.NewString()
				committedAt := time.Now()
				prefetchedRecent, prefetchedReadyAt, prefetchedReady := getMemoryPrefetch()
				_ = o.sessions.StartTurn(s.ID, turnID)
				assistantOutputStartedAt.Store(0)
				turnCtx, cancel := context.WithCancel(ctx)

				turnMu.Lock()
				nextToken++
				token := nextToken
				turnCancel = cancel
				activeTurnID = turnID
				activeToken = token
				turnMu.Unlock()

				go func(turnText, turnID string, token int64, committedAt time.Time, prefetchedRecent []memory.TurnRecord, prefetchedReadyAt time.Time, prefetchedReady bool, prefetchedBrain *brainPrefetchResult) {
					defer func() {
						turnMu.Lock()
						if activeToken == token {
							turnCancel = nil
							activeTurnID = ""
							activeToken = 0
						}
						turnMu.Unlock()
					}()

					if err := o.runAssistantTurn(turnCtx, *s, turnText, turnID, committedAt, prefetchedRecent, prefetchedReadyAt, prefetchedReady, prefetchedBrain, func() {
						assistantOutputStartedAt.CompareAndSwap(0, time.Now().UnixMilli())
					}, outbound); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						o.send(outbound, protocol.ErrorEvent{
							Type:      protocol.TypeErrorEvent,
							SessionID: s.ID,
							Code:      "assistant_turn_failed",
							Source:    "orchestrator",
							Retryable: false,
							Detail:    err.Error(),
						})
						o.send(outbound, protocol.AssistantTurnEnd{
							Type:      protocol.TypeAssistantTurnEnd,
							SessionID: s.ID,
							TurnID:    turnID,
							Reason:    "failed",
						})
					}
				}(committedText, turnID, token, committedAt, prefetchedRecent, prefetchedReadyAt, prefetchedReady, prefetchedBrain)
			case STTEventError:
				if evt.Code == "commit_throttled" {
					// Non-fatal: happens when commit is requested with too little uncommitted audio.
					continue
				}
				o.metrics.ProviderErrors.WithLabelValues(o.sttLabel, evt.Code).Inc()
				o.send(outbound, protocol.ErrorEvent{
					Type:      protocol.TypeErrorEvent,
					SessionID: s.ID,
					Code:      evt.Code,
					Source:    "stt",
					Retryable: evt.Retryable,
					Detail:    evt.Detail,
				})
			}
		}
	}
}

func (o *Orchestrator) runAssistantTurn(
	ctx context.Context,
	s session.Session,
	userText, turnID string,
	committedAt time.Time,
	prefetchedRecent []memory.TurnRecord,
	prefetchedReadyAt time.Time,
	prefetchedReady bool,
	prefetchedBrain *brainPrefetchResult,
	markAssistantOutputStart func(),
	outbound chan<- any,
) error {
	start := time.Now()
	profile := o.profileForSession(s)

	redactedUserText, userChanged := policy.RedactPII(userText)
	o.saveTurnBestEffort(memory.TurnRecord{
		ID:          uuid.NewString(),
		UserID:      s.UserID,
		SessionID:   s.ID,
		Role:        "user",
		Content:     redactedUserText,
		PIIRedacted: userChanged,
		CreatedAt:   time.Now().UTC(),
	})

	settings := ttsSettingsForProfile(profile)
	type ttsPreflightResult struct {
		stream  TTSStream
		err     error
		readyAt time.Time
	}
	type memoryPreflightResult struct {
		recent  []memory.TurnRecord
		err     error
		readyAt time.Time
	}

	ttsResCh := make(chan ttsPreflightResult, 1)
	go func() {
		stream, err := o.ttsProvider.StartStream(ctx, profile.VoiceID, profile.ModelID, settings)
		ttsResCh <- ttsPreflightResult{
			stream:  stream,
			err:     err,
			readyAt: time.Now(),
		}
	}()

	memResCh := make(chan memoryPreflightResult, 1)
	skipMemoryForPrefetchedBrain := prefetchedBrain != nil
	memReady := o.memoryStore == nil || skipMemoryForPrefetchedBrain
	memRes := memoryPreflightResult{readyAt: time.Now()}
	if skipMemoryForPrefetchedBrain {
		o.metrics.SessionEvents.WithLabelValues("memory_context_skipped_prefetched_brain").Inc()
	} else if prefetchedReady {
		memReady = true
		memRes = memoryPreflightResult{
			recent:  prefetchedRecent,
			readyAt: prefetchedReadyAt,
		}
		if memRes.readyAt.IsZero() {
			memRes.readyAt = time.Now()
		}
		o.metrics.SessionEvents.WithLabelValues("memory_context_prefetch_hit").Inc()
	} else if o.memoryStore != nil {
		o.metrics.SessionEvents.WithLabelValues("memory_context_prefetch_miss").Inc()
	}
	memCancel := func() {}
	if o.memoryStore != nil && !memReady {
		memCtx, cancel := context.WithTimeout(ctx, memoryContextTimeout)
		memCancel = cancel
		go func() {
			defer cancel()
			recent, err := o.memoryStore.RecentContext(memCtx, s.UserID, memoryContextLimit)
			memResCh <- memoryPreflightResult{
				recent:  recent,
				err:     err,
				readyAt: time.Now(),
			}
		}()
	}

	if o.memoryStore != nil && !prefetchedReady && !skipMemoryForPrefetchedBrain {
		waitBudget := memoryContextSoftWait - time.Since(committedAt)
		if waitBudget < 0 {
			waitBudget = 0
		}
		timer := time.NewTimer(waitBudget)
		select {
		case memRes = <-memResCh:
			memReady = true
		case <-timer.C:
			memCancel()
			o.metrics.SessionEvents.WithLabelValues("memory_context_skipped").Inc()
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	if memReady && !memRes.readyAt.IsZero() {
		if d := memRes.readyAt.Sub(committedAt); d > 0 {
			o.metrics.ObserveTurnStage("commit_to_context_ready", d)
		}
	}
	if memReady && memRes.err != nil && !errors.Is(memRes.err, context.Canceled) {
		if errors.Is(memRes.err, context.DeadlineExceeded) {
			o.metrics.SessionEvents.WithLabelValues("memory_context_timeout").Inc()
		} else {
			o.metrics.SessionEvents.WithLabelValues("memory_context_error").Inc()
		}
	}

	recent := memRes.recent
	contextLines := make([]string, 0, len(recent))
	for _, r := range recent {
		contextLines = append(contextLines, fmt.Sprintf("%s: %s", r.Role, r.Content))
	}

	var (
		ttsStream      TTSStream
		ttsErr         error
		ttsReady       bool
		ttsDone        chan struct{}
		ttsShouldClose bool
	)
	defer func() {
		if ttsShouldClose && ttsStream != nil {
			_ = ttsStream.Close()
		}
	}()

	firstAudioObserved := false
	firstTextObserved := false
	brainFirstDeltaObserved := false
	brainFirstDeltaToAudioObserved := false
	var brainFirstDeltaAt time.Time
	var assistantOutputStartOnce sync.Once
	markAssistantStarted := func() {
		if markAssistantOutputStart == nil {
			return
		}
		assistantOutputStartOnce.Do(markAssistantOutputStart)
	}
	speechProduced := false
	speechSent := false
	var speechPending strings.Builder
	speechSan := newSpeechSanitizer()
	prosody := newProsodyPlanner()
	firstThinkingDeltaObserved := false
	lastThinkingDelta := ""
	firstTextSignal := make(chan struct{})
	var firstTextSignalOnce sync.Once
	markFirstTextObserved := func() {
		firstTextSignalOnce.Do(func() {
			close(firstTextSignal)
		})
	}
	defer markFirstTextObserved()

	startTTSForwarder := func(stream TTSStream) {
		if ttsDone != nil || stream == nil {
			return
		}
		ttsDone = make(chan struct{})
		go func() {
			defer close(ttsDone)
			seq := 0
			for {
				select {
				case <-ctx.Done():
					// Stop forwarding audio immediately on interruption; providers may still emit
					// buffered chunks but the client should pivot to the new user utterance.
					return
				case evt, ok := <-stream.Events():
					if !ok {
						return
					}
					switch evt.Type {
					case TTSEventAudio:
						if !firstAudioObserved {
							firstAudioObserved = true
							markAssistantStarted()
							o.metrics.ObserveTurnStage("commit_to_first_audio", time.Since(committedAt))
							o.metrics.ObserveFirstAudioLatency(time.Since(start))
							o.metrics.ObserveTurnIndicator("assistant_first_audio")
							o.send(outbound, protocol.SystemEvent{
								Type:      protocol.TypeSystemEvent,
								SessionID: s.ID,
								Code:      "assistant_first_audio",
							})
							if brainFirstDeltaObserved && !brainFirstDeltaToAudioObserved && !brainFirstDeltaAt.IsZero() {
								brainFirstDeltaToAudioObserved = true
								o.metrics.ObserveTurnStage("brain_first_delta_to_first_audio", time.Since(brainFirstDeltaAt))
							}
						}
						seq++
						o.send(outbound, protocol.AssistantAudioChunk{
							Type:        protocol.TypeAssistantAudio,
							SessionID:   s.ID,
							TurnID:      turnID,
							Seq:         seq,
							Format:      evt.Format,
							AudioBase64: evt.AudioBase64,
						})
					case TTSEventError:
						o.metrics.ProviderErrors.WithLabelValues(o.ttsLabel, evt.Code).Inc()
						o.send(outbound, protocol.ErrorEvent{
							Type:      protocol.TypeErrorEvent,
							SessionID: s.ID,
							Code:      evt.Code,
							Source:    "tts",
							Retryable: evt.Retryable,
							Detail:    evt.Detail,
						})
					case TTSEventFinal:
						return
					}
				}
			}
		}()
	}

	adoptTTSResult := func(block bool) bool {
		if ttsReady {
			return true
		}
		var (
			res ttsPreflightResult
			ok  bool
		)
		if block {
			select {
			case <-ctx.Done():
				return false
			case res, ok = <-ttsResCh:
				if !ok {
					return false
				}
			}
		} else {
			select {
			case res, ok = <-ttsResCh:
				if !ok {
					return false
				}
			default:
				return false
			}
		}

		ttsReady = true
		ttsStream = res.stream
		ttsErr = res.err
		if !res.readyAt.IsZero() {
			if d := res.readyAt.Sub(committedAt); d > 0 {
				o.metrics.ObserveTurnStage("commit_to_tts_ready", d)
			}
		}
		if ttsErr != nil {
			o.metrics.SessionEvents.WithLabelValues("tts_start_failed").Inc()
			o.send(outbound, protocol.ErrorEvent{
				Type:      protocol.TypeErrorEvent,
				SessionID: s.ID,
				Code:      "tts_start_failed",
				Source:    "tts",
				Retryable: true,
				Detail:    ttsErr.Error(),
			})
			return true
		}
		ttsShouldClose = true
		startTTSForwarder(ttsStream)
		return true
	}

	flushPendingSpeech := func() error {
		if speechPending.Len() == 0 {
			return nil
		}
		if !ttsReady || ttsErr != nil || ttsStream == nil {
			return nil
		}
		out := speechPending.String()
		speechPending.Reset()
		if strings.TrimSpace(out) == "" {
			return nil
		}
		speechSent = true
		return ttsStream.SendText(ctx, out, true)
	}

	queueSpeechSegment := func(segment string) error {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil
		}
		_ = adoptTTSResult(false)
		if err := flushPendingSpeech(); err != nil {
			return err
		}
		if ttsReady && ttsErr == nil && ttsStream != nil {
			speechSent = true
			return ttsStream.SendText(ctx, segment, true)
		}
		if speechPending.Len() > 0 {
			lastByte := speechPending.String()[speechPending.Len()-1]
			if lastByte != ' ' && lastByte != '\n' {
				speechPending.WriteByte(' ')
			}
		}
		speechPending.WriteString(segment)
		return nil
	}

	emitThinkingDelta := func(rawDelta string) {
		if firstTextObserved {
			return
		}
		preview := thinkingDeltaPreview(rawDelta)
		if preview == "" || preview == lastThinkingDelta {
			return
		}
		lastThinkingDelta = preview
		if !firstThinkingDeltaObserved {
			firstThinkingDeltaObserved = true
			o.metrics.ObserveTurnStage("commit_to_thinking_delta", time.Since(committedAt))
			o.metrics.ObserveTurnIndicator("assistant_thinking_delta")
		}
		o.send(outbound, protocol.AssistantThinkingDelta{
			Type:      protocol.TypeAssistantThinkingDelta,
			SessionID: s.ID,
			TurnID:    turnID,
			TextDelta: preview,
		})
	}

	// Keep voice UX responsive: if the model hasn't started producing text quickly,
	// emit a short visual progress cue instead of leaving dead air.
	if o.assistantWorkingDelay > 0 {
		go func() {
			timer := time.NewTimer(o.assistantWorkingDelay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-firstTextSignal:
				return
			case <-timer.C:
				o.metrics.ObserveTurnStage("commit_to_assistant_working", time.Since(committedAt))
				o.metrics.ObserveTurnIndicator("assistant_working")
				o.send(outbound, protocol.SystemEvent{
					Type:      protocol.TypeSystemEvent,
					SessionID: s.ID,
					Code:      "assistant_working",
				})
			}
		}()
	}

	var assistantOut string
	leadFilter := newLeadResponseFilter()
	handleDelta := func(delta string) error {
		if !brainFirstDeltaObserved && strings.TrimSpace(delta) != "" {
			brainFirstDeltaObserved = true
			brainFirstDeltaAt = time.Now()
			o.metrics.ObserveTurnStage("commit_to_brain_first_delta", time.Since(committedAt))
		}
		emitThinkingDelta(delta)
		delta = leadFilter.Consume(delta)
		if strings.TrimSpace(delta) == "" {
			return nil
		}
		assistantOut += delta
		if !firstTextObserved && strings.TrimSpace(delta) != "" {
			firstTextObserved = true
			markAssistantStarted()
			markFirstTextObserved()
			o.metrics.ObserveTurnStage("commit_to_first_text", time.Since(committedAt))
			o.metrics.ObserveTurnIndicator("assistant_first_text")
			o.send(outbound, protocol.SystemEvent{
				Type:      protocol.TypeSystemEvent,
				SessionID: s.ID,
				Code:      "assistant_first_text",
			})
		}
		o.send(outbound, protocol.AssistantTextDelta{
			Type:      protocol.TypeAssistantTextDelta,
			SessionID: s.ID,
			TurnID:    turnID,
			TextDelta: delta,
		})
		speechDelta := speechSan.SanitizeDelta(delta)
		if speechDelta == "" {
			return nil
		}
		speechDelta = bridgeSpeechDelta(delta, speechDelta, speechProduced)
		speechProduced = true
		segments := prosody.Push(speechDelta)
		for _, segment := range segments {
			if err := queueSpeechSegment(segment); err != nil {
				return err
			}
		}
		return nil
	}

	var res openclaw.MessageResponse
	if prefetchedBrain != nil {
		o.metrics.SessionEvents.WithLabelValues("brain_prefetch_hit").Inc()
		o.metrics.ObserveTurnIndicator("brain_prefetch_hit")
		for _, delta := range prefetchedBrain.deltas {
			if err := handleDelta(delta); err != nil {
				_ = ttsStream.CloseInput(ctx)
				return err
			}
		}
		// Some adapters return only final text for speculative runs (no incremental deltas).
		// Emit it through the same delta path so first-delta metrics and speech streaming
		// still reflect this fast prefetch-hit path.
		if strings.TrimSpace(prefetchedBrain.finalText) != "" && strings.TrimSpace(assistantOut) == "" {
			if err := handleDelta(prefetchedBrain.finalText); err != nil {
				_ = ttsStream.CloseInput(ctx)
				return err
			}
		}
		res = openclaw.MessageResponse{Text: prefetchedBrain.finalText}
	} else {
		o.metrics.SessionEvents.WithLabelValues("brain_prefetch_miss").Inc()
		o.metrics.ObserveTurnIndicator("brain_prefetch_miss")
		retries := 0
		var streamErr error
		retryTimeout := brainFirstDeltaRetryTimeout
		retryMax := brainFirstDeltaRetryMax
		if _, ok := o.adapter.(*openclaw.FallbackAdapter); ok {
			// FallbackAdapter already enforces first-delta timeout and failover; avoid
			// double-canceling the request before fallback can complete.
			retryTimeout = 0
			retryMax = 0
		}
		res, retries, streamErr = streamResponseWithFirstDeltaRetry(
			ctx,
			o.adapter,
			openclaw.MessageRequest{
				UserID:        s.UserID,
				SessionID:     s.ID,
				TurnID:        turnID,
				InputText:     userText,
				MemoryContext: contextLines,
				PersonaID:     profile.ID,
			},
			handleDelta,
			retryTimeout,
			retryMax,
		)
		for i := 0; i < retries; i++ {
			o.metrics.SessionEvents.WithLabelValues("brain_first_delta_retry").Inc()
		}
		if streamErr != nil {
			return fmt.Errorf("stream response: %w", streamErr)
		}
	}
	if assistantOut == "" {
		assistantOut = leadFilter.Finalize(res.Text)
		if assistantOut == "" {
			assistantOut = strings.TrimSpace(res.Text)
		}
		if assistantOut != "" {
			if !firstTextObserved && strings.TrimSpace(assistantOut) != "" {
				firstTextObserved = true
				markAssistantStarted()
				markFirstTextObserved()
				o.metrics.ObserveTurnStage("commit_to_first_text", time.Since(committedAt))
				o.metrics.ObserveTurnIndicator("assistant_first_text")
				o.send(outbound, protocol.SystemEvent{
					Type:      protocol.TypeSystemEvent,
					SessionID: s.ID,
					Code:      "assistant_first_text",
				})
			}
			o.send(outbound, protocol.AssistantTextDelta{
				Type:      protocol.TypeAssistantTextDelta,
				SessionID: s.ID,
				TurnID:    turnID,
				TextDelta: assistantOut,
			})
			speechOut := speechSan.SanitizeDelta(assistantOut)
			if speechOut != "" {
				speechOut = bridgeSpeechDelta(assistantOut, speechOut, speechProduced)
				speechProduced = true
				segments := prosody.Push(speechOut)
				for _, segment := range segments {
					if err := queueSpeechSegment(segment); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, segment := range prosody.Finalize() {
		if err := queueSpeechSegment(segment); err != nil {
			return err
		}
	}
	_ = adoptTTSResult(true)
	if err := flushPendingSpeech(); err != nil {
		return err
	}
	if ttsReady && ttsErr == nil && ttsStream != nil {
		if !speechSent && strings.TrimSpace(assistantOut) != "" {
			speechSent = true
			_ = ttsStream.SendText(ctx, "I replied on screen.", true)
		}
		_ = ttsStream.CloseInput(ctx)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ttsDone:
		case <-time.After(ttsFinalizeTimeout):
			return fmt.Errorf("tts finalization timeout")
		}
	}

	if strings.TrimSpace(assistantOut) != "" {
		redactedAssistantText, assistantChanged := policy.RedactPII(assistantOut)
		o.saveTurnBestEffort(memory.TurnRecord{
			ID:          uuid.NewString(),
			UserID:      s.UserID,
			SessionID:   s.ID,
			Role:        "assistant",
			Content:     redactedAssistantText,
			PIIRedacted: assistantChanged,
			CreatedAt:   time.Now().UTC(),
		})
	}

	o.send(outbound, protocol.AssistantTurnEnd{
		Type:      protocol.TypeAssistantTurnEnd,
		SessionID: s.ID,
		TurnID:    turnID,
		Reason:    "completed",
	})

	if o.firstAudioSLO > 0 && firstAudioObserved {
		lat := time.Since(start)
		if lat > o.firstAudioSLO {
			o.metrics.SessionEvents.WithLabelValues("first_audio_slo_miss").Inc()
		}
	}
	o.metrics.ObserveTurnStage("turn_total", time.Since(committedAt))

	return nil
}

func (o *Orchestrator) profileForSession(s session.Session) PersonaProfile {
	if s.PersonaID != "" {
		if p, ok := o.profiles[s.PersonaID]; ok {
			if p.VoiceID == "" {
				p.VoiceID = o.defaultVoice
			}
			if p.ModelID == "" {
				p.ModelID = o.defaultModel
			}
			if s.VoiceID != "" {
				p.VoiceID = s.VoiceID
			}
			return p
		}
	}
	p := o.profiles["warm"]
	if p.VoiceID == "" {
		p.VoiceID = o.defaultVoice
	}
	if p.ModelID == "" {
		p.ModelID = o.defaultModel
	}
	if s.VoiceID != "" {
		p.VoiceID = s.VoiceID
	}
	return p
}

func (o *Orchestrator) profileForPersona(personaID string) PersonaProfile {
	personaID = strings.TrimSpace(personaID)
	if personaID != "" {
		if p, ok := o.profiles[personaID]; ok {
			if p.VoiceID == "" {
				p.VoiceID = o.defaultVoice
			}
			if p.ModelID == "" {
				p.ModelID = o.defaultModel
			}
			return p
		}
	}

	p := o.profiles["warm"]
	if p.VoiceID == "" {
		p.VoiceID = o.defaultVoice
	}
	if p.ModelID == "" {
		p.ModelID = o.defaultModel
	}
	return p
}

func (o *Orchestrator) send(outbound chan<- any, msg any) {
	msgType, critical := outboundMessageMeta(msg)
	record := func(result string) {
		o.metrics.ObserveOutboundMessage(msgType, result)
	}

	sendWithTimeout := func(timeout time.Duration, timeoutEvent string) bool {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case outbound <- msg:
			record("delivered")
			return true
		case <-timer.C:
			record("timeout")
			if timeoutEvent != "" {
				o.metrics.SessionEvents.WithLabelValues(timeoutEvent).Inc()
			}
			return false
		}
	}

	criticalTimeout := 600 * time.Millisecond
	if critical {
		if !sendWithTimeout(criticalTimeout, "outbound_timeout_critical") {
			o.metrics.SessionEvents.WithLabelValues("outbound_drop").Inc()
		}
		return
	}

	if !o.strictOutbound {
		select {
		case outbound <- msg:
			record("delivered")
		default:
			record("dropped")
			o.metrics.SessionEvents.WithLabelValues("outbound_drop").Inc()
		}
		return
	}

	if o.outboundMode == "block" {
		if !sendWithTimeout(120*time.Millisecond, "outbound_timeout") {
			o.metrics.SessionEvents.WithLabelValues("outbound_drop").Inc()
		}
		return
	}

	// Strict+drop: drop low-priority bursts, but never critical events above.
	select {
	case outbound <- msg:
		record("delivered")
	default:
		record("dropped")
		o.metrics.SessionEvents.WithLabelValues("outbound_drop").Inc()
	}
}

func (o *Orchestrator) sendTaskEvent(outbound chan<- any, evt tasks.Event) {
	switch evt.Type {
	case tasks.EventTaskCreated:
		o.send(outbound, protocol.TaskCreated{
			Type:             protocol.TypeTaskCreated,
			SessionID:        evt.SessionID,
			TaskID:           evt.TaskID,
			Summary:          evt.Title,
			Status:           string(evt.Status),
			RiskLevel:        string(evt.RiskLevel),
			RequiresApproval: evt.RequiresApproval,
		})
	case tasks.EventTaskPlanGraph:
		nodes := make([]protocol.TaskPlanGraphNode, 0)
		edges := make([]protocol.TaskPlanGraphEdge, 0)
		if evt.Graph != nil {
			nodes = make([]protocol.TaskPlanGraphNode, 0, len(evt.Graph.Nodes))
			for _, n := range evt.Graph.Nodes {
				nodes = append(nodes, protocol.TaskPlanGraphNode{
					ID:               n.ID,
					Seq:              n.Seq,
					Title:            n.Title,
					Kind:             n.Kind,
					Status:           string(n.Status),
					RiskLevel:        string(n.RiskLevel),
					RequiresApproval: n.RequiresApproval,
				})
			}
			edges = make([]protocol.TaskPlanGraphEdge, 0, len(evt.Graph.Edges))
			for _, e := range evt.Graph.Edges {
				edges = append(edges, protocol.TaskPlanGraphEdge{
					From: e.From,
					To:   e.To,
					Kind: e.Kind,
				})
			}
		}
		o.send(outbound, protocol.TaskPlanGraph{
			Type:      protocol.TypeTaskPlanGraph,
			SessionID: evt.SessionID,
			TaskID:    evt.TaskID,
			Status:    string(evt.Status),
			Detail:    evt.Detail,
			Nodes:     nodes,
			Edges:     edges,
		})
	case tasks.EventTaskPlanDelta:
		o.send(outbound, protocol.TaskPlanDelta{
			Type:           protocol.TypeTaskPlanDelta,
			SessionID:      evt.SessionID,
			TaskID:         evt.TaskID,
			Status:         string(evt.Status),
			TextDelta:      evt.TextDelta,
			QueuedPosition: evt.QueuedPosition,
		})
	case tasks.EventTaskStepStarted:
		o.send(outbound, protocol.TaskStepStarted{
			Type:      protocol.TypeTaskStepStarted,
			SessionID: evt.SessionID,
			TaskID:    evt.TaskID,
			StepID:    evt.StepID,
			StepSeq:   evt.StepSeq,
			Title:     evt.Title,
			RiskLevel: string(evt.RiskLevel),
		})
	case tasks.EventTaskStepLog:
		o.send(outbound, protocol.TaskStepLog{
			Type:      protocol.TypeTaskStepLog,
			SessionID: evt.SessionID,
			TaskID:    evt.TaskID,
			StepID:    evt.StepID,
			TextDelta: evt.TextDelta,
		})
	case tasks.EventTaskStepCompleted:
		o.send(outbound, protocol.TaskStepCompleted{
			Type:      protocol.TypeTaskStepCompleted,
			SessionID: evt.SessionID,
			TaskID:    evt.TaskID,
			StepID:    evt.StepID,
			Status:    string(evt.Status),
		})
	case tasks.EventTaskWaitingApproval:
		o.send(outbound, protocol.TaskWaitingApproval{
			Type:             protocol.TypeTaskWaitingApproval,
			SessionID:        evt.SessionID,
			TaskID:           evt.TaskID,
			StepID:           evt.StepID,
			RiskLevel:        string(evt.RiskLevel),
			Prompt:           evt.Prompt,
			RequiresApproval: evt.RequiresApproval,
		})
	case tasks.EventTaskCompleted:
		o.send(outbound, protocol.TaskCompleted{
			Type:      protocol.TypeTaskCompleted,
			SessionID: evt.SessionID,
			TaskID:    evt.TaskID,
			Status:    string(evt.Status),
			Result:    evt.Result,
		})
	case tasks.EventTaskFailed:
		o.send(outbound, protocol.TaskFailed{
			Type:      protocol.TypeTaskFailed,
			SessionID: evt.SessionID,
			TaskID:    evt.TaskID,
			StepID:    evt.StepID,
			Status:    string(evt.Status),
			Code:      evt.Code,
			Detail:    evt.Detail,
		})
	}
}

func (o *Orchestrator) taskStatusSnapshotForSession(sessionID string) protocol.TaskStatusSnapshot {
	snapshot := protocol.TaskStatusSnapshot{
		Type:             protocol.TypeTaskStatusSnapshot,
		SessionID:        sessionID,
		Active:           []protocol.TaskStatusSnapshotItem{},
		AwaitingApproval: []protocol.TaskStatusSnapshotItem{},
		Planned:          []protocol.TaskStatusSnapshotItem{},
	}
	if o.taskService == nil || !o.taskService.Enabled() {
		return snapshot
	}

	tasksBySession := o.taskService.ListTasks(sessionID, 100)
	for _, task := range tasksBySession {
		item := protocol.TaskStatusSnapshotItem{
			TaskID:           task.ID,
			Summary:          task.Summary,
			Status:           string(task.Status),
			RiskLevel:        string(task.RiskLevel),
			RequiresApproval: task.RequiresApproval,
		}
		switch task.Status {
		case tasks.TaskStatusRunning:
			snapshot.Active = append(snapshot.Active, item)
		case tasks.TaskStatusAwaitingApproval:
			snapshot.AwaitingApproval = append(snapshot.AwaitingApproval, item)
		case tasks.TaskStatusPlanned, tasks.TaskStatusPaused:
			snapshot.Planned = append(snapshot.Planned, item)
		}
	}
	return snapshot
}

func outboundMessageMeta(msg any) (msgType string, critical bool) {
	switch m := msg.(type) {
	case protocol.AssistantTurnEnd:
		return string(m.Type), true
	case protocol.ErrorEvent:
		return string(m.Type), true
	case protocol.SystemEvent:
		return string(m.Type), true
	case protocol.AssistantAudioChunk:
		return string(m.Type), false
	case protocol.AssistantThinkingDelta:
		return string(m.Type), false
	case protocol.SemanticEndpointHint:
		return string(m.Type), false
	case protocol.AssistantTextDelta:
		return string(m.Type), false
	case protocol.STTPartial:
		return string(m.Type), false
	case protocol.STTCommitted:
		return string(m.Type), false
	case protocol.TaskCreated:
		return string(m.Type), true
	case protocol.TaskPlanGraph:
		return string(m.Type), false
	case protocol.TaskPlanDelta:
		return string(m.Type), false
	case protocol.TaskStepStarted:
		return string(m.Type), true
	case protocol.TaskStepLog:
		return string(m.Type), false
	case protocol.TaskStepCompleted:
		return string(m.Type), true
	case protocol.TaskWaitingApproval:
		return string(m.Type), true
	case protocol.TaskCompleted:
		return string(m.Type), true
	case protocol.TaskFailed:
		return string(m.Type), true
	case protocol.TaskStatusSnapshot:
		return string(m.Type), true
	default:
		return "unknown", false
	}
}

func normalizeControlReason(raw string) string {
	reason := strings.ToLower(strings.TrimSpace(raw))
	if reason == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(reason))
	prevUnderscore := false
	for _, r := range reason {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	return out
}

func (o *Orchestrator) prewarmAdapter() brainPrewarmCapable {
	if o == nil || o.adapter == nil {
		return nil
	}
	if warm, ok := o.adapter.(brainPrewarmCapable); ok {
		return warm
	}
	if fb, ok := o.adapter.(*openclaw.FallbackAdapter); ok {
		if primary := fb.Primary(); primary != nil {
			if warm, ok := primary.(brainPrewarmCapable); ok {
				return warm
			}
		}
	}
	return nil
}

func (o *Orchestrator) startBrainSessionWarmup(parent context.Context, sessionID string) {
	warm := o.prewarmAdapter()
	if warm == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if o.metrics != nil && o.metrics.SessionEvents != nil {
		o.metrics.SessionEvents.WithLabelValues("brain_warmup_start").Inc()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, brainWarmupTimeout)
		defer cancel()
		if err := warm.PrewarmSession(ctx, sessionID); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if o.metrics != nil && o.metrics.SessionEvents != nil {
					o.metrics.SessionEvents.WithLabelValues("brain_warmup_timeout").Inc()
				}
				return
			}
			if o.metrics != nil && o.metrics.SessionEvents != nil {
				o.metrics.SessionEvents.WithLabelValues("brain_warmup_failed").Inc()
			}
			return
		}
		if o.metrics != nil && o.metrics.SessionEvents != nil {
			o.metrics.SessionEvents.WithLabelValues("brain_warmup_ok").Inc()
		}
	}()
}

func streamResponseWithFirstDeltaRetry(
	ctx context.Context,
	adapter openclaw.Adapter,
	req openclaw.MessageRequest,
	onDelta openclaw.DeltaHandler,
	firstDeltaTimeout time.Duration,
	maxRetries int,
) (openclaw.MessageResponse, int, error) {
	if adapter == nil {
		return openclaw.MessageResponse{}, 0, fmt.Errorf("adapter is nil")
	}
	if firstDeltaTimeout <= 0 || maxRetries <= 0 {
		resp, err := adapter.StreamResponse(ctx, req, onDelta)
		return resp, 0, err
	}

	retries := 0
	for attempt := 0; attempt <= maxRetries; attempt++ {
		attemptReq := req
		if attempt > 0 {
			baseTurnID := strings.TrimSpace(req.TurnID)
			if baseTurnID == "" {
				attemptReq.TurnID = "retry-" + uuid.NewString()
			} else {
				attemptReq.TurnID = fmt.Sprintf("%s-retry-%d", baseTurnID, attempt)
			}
		}

		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		var (
			firstDeltaSeen atomic.Bool
			timedOut       atomic.Bool
		)
		timer := time.AfterFunc(firstDeltaTimeout, func() {
			if !firstDeltaSeen.Load() {
				timedOut.Store(true)
				cancelAttempt()
			}
		})

		resp, err := adapter.StreamResponse(attemptCtx, attemptReq, func(delta string) error {
			if strings.TrimSpace(delta) != "" {
				firstDeltaSeen.Store(true)
			}
			if onDelta == nil {
				return nil
			}
			return onDelta(delta)
		})
		timer.Stop()
		cancelAttempt()

		if err == nil {
			return resp, retries, nil
		}
		if timedOut.Load() && !firstDeltaSeen.Load() && retries >= maxRetries {
			return openclaw.MessageResponse{}, retries, fmt.Errorf("%w after %d attempt(s) (%s)", errBrainFirstDeltaTimeout, retries+1, firstDeltaTimeout)
		}
		if !timedOut.Load() || firstDeltaSeen.Load() {
			return openclaw.MessageResponse{}, retries, err
		}
		retries++
	}

	return openclaw.MessageResponse{}, retries, context.DeadlineExceeded
}

func ttsSettingsForProfile(p PersonaProfile) TTSSettings {
	// ElevenLabs voice settings are in [0,1]. Lower stability generally yields more expressive
	// (but less consistent) speech; we tie it loosely to "warmth".
	stability := 0.32 + (1.0-clampFloat(p.Warmth, 0, 1))*0.45
	return TTSSettings{
		Stability:       clampFloat(stability, 0.25, 0.75),
		SimilarityBoost: 0.9,
		Speed:           clampFloat(p.SpeakingRate, 0.7, 1.2),
	}
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (o *Orchestrator) saveTurnBestEffort(record memory.TurnRecord) {
	if o.memoryStore == nil {
		return
	}
	go func(r memory.TurnRecord) {
		saveCtx, cancel := context.WithTimeout(context.Background(), memorySaveTimeout)
		defer cancel()
		if err := o.memoryStore.SaveTurn(saveCtx, r); err != nil {
			o.metrics.SessionEvents.WithLabelValues("memory_save_failed").Inc()
		}
	}(record)
}

var wakeWordPrefixRe = regexp.MustCompile(`(?i)^\s*(?:(?:hey|hi|ok|okay)\b[\s,.:;!?-]*)?samantha\b[\s,.:;!?-]*\s*(.*)$`)

func detectWakeWordCommitted(text string) (bool, string) {
	// Keep this intentionally conservative: only accept wake word near the start to avoid
	// triggering on unrelated conversation. Return the remainder in the original form.
	m := wakeWordPrefixRe.FindStringSubmatch(text)
	if len(m) == 0 {
		return false, ""
	}
	return true, strings.TrimSpace(m[1])
}

func canonicalizeBrainPrefetchInput(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))

	prevSpace := true
	for _, r := range strings.ToLower(raw) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			// Ignore symbols/emoji for matching.
		}
	}
	return strings.TrimSpace(b.String())
}

func canonicalIsProgressiveContinuation(prev, next string) bool {
	prev = strings.TrimSpace(prev)
	next = strings.TrimSpace(next)
	if prev == "" || next == "" {
		return false
	}
	if prev == next {
		return true
	}
	// Common path for partial STT growth: "build api" -> "build api endpoint".
	if strings.HasPrefix(next, prev) {
		return true
	}
	// Accept very small rewinds caused by STT punctuation/word correction.
	if strings.HasPrefix(prev, next) {
		const maxRollbackChars = 6
		return len(prev)-len(next) <= maxRollbackChars
	}
	return false
}

func shouldSpeculateBrainCanonical(canonical string) bool {
	canonical = strings.TrimSpace(canonical)
	if len(canonical) < brainPrefetchMinCanonical {
		return false
	}
	words := 1
	for i := 0; i < len(canonical); i++ {
		if canonical[i] == ' ' {
			words++
		}
	}
	return words >= brainPrefetchMinWords
}

func brainPrefetchCanonicalCompatible(prefetchedCanonical, committedCanonical string) bool {
	prefetchedCanonical = strings.TrimSpace(prefetchedCanonical)
	committedCanonical = strings.TrimSpace(committedCanonical)
	if prefetchedCanonical == "" || committedCanonical == "" {
		return false
	}
	if prefetchedCanonical == committedCanonical {
		return true
	}
	// Common case: one canonical transcript is an incremental extension/rollback of the other.
	if canonicalIsProgressiveContinuation(prefetchedCanonical, committedCanonical) ||
		canonicalIsProgressiveContinuation(committedCanonical, prefetchedCanonical) {
		shared := sharedWordPrefixCount(strings.Fields(prefetchedCanonical), strings.Fields(committedCanonical))
		if shared >= 2 {
			return true
		}
	}
	// Guarded fuzzy match for tiny STT corrections:
	// require a strong shared word prefix and only allow one trailing mismatch.
	pWords := strings.Fields(prefetchedCanonical)
	cWords := strings.Fields(committedCanonical)
	minWords := len(pWords)
	if len(cWords) < minWords {
		minWords = len(cWords)
	}
	if minWords < 4 {
		return false
	}
	if absInt(len(pWords)-len(cWords)) > 2 {
		return false
	}
	shared := sharedWordPrefixCount(pWords, cWords)
	return shared >= minWords-1 && shared >= 3
}

func shouldKeepBrainPrefetchInFlight(inFlightCanonical, incomingCanonical string) bool {
	inFlightCanonical = strings.TrimSpace(inFlightCanonical)
	incomingCanonical = strings.TrimSpace(incomingCanonical)
	if inFlightCanonical == "" || incomingCanonical == "" {
		return false
	}
	if inFlightCanonical == incomingCanonical {
		return true
	}
	inFlightWords := wordsInCanonical(inFlightCanonical)
	incomingWords := wordsInCanonical(incomingCanonical)
	// If STT collapses the utterance by multiple words, treat it as a real rewrite and restart.
	if incomingWords+1 < inFlightWords {
		return false
	}
	if compactCanonical(inFlightCanonical) == compactCanonical(incomingCanonical) {
		return true
	}
	// Keep the speculative run when canonical transcripts are still strongly compatible.
	// This avoids cancel/restart churn from small STT rewrites ("end point" -> "endpoint").
	if brainPrefetchCanonicalCompatible(inFlightCanonical, incomingCanonical) {
		return true
	}
	// Keep an in-flight speculative response when the new transcript is an extension of
	// the existing canonical input. This prevents cancel/restart churn while the user is
	// still speaking and materially increases prefetch hit probability at commit time.
	if canonicalIsProgressiveContinuation(inFlightCanonical, incomingCanonical) {
		if sharedWordPrefixCount(strings.Fields(inFlightCanonical), strings.Fields(incomingCanonical)) >= 2 {
			return true
		}
	}
	return false
}

func compactCanonical(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sharedWordPrefixCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	count := 0
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			break
		}
		count++
	}
	return count
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func shouldStartBrainPrefetchEarly(partialText, canonical string, utteranceAge time.Duration) bool {
	words := wordsInCanonical(canonical)
	normalized := normalizeSemanticEndpointText(partialText)
	if !shouldSpeculateBrainCanonical(canonical) {
		// For short terminal commands, allow one early speculation if we still have enough
		// language signal to avoid noisy one-word prefetch churn.
		if hasSemanticTerminalCue(normalized) && words >= brainPrefetchMinWords && len(canonical) >= 12 {
			return true
		}
		return false
	}
	if words >= brainPrefetchEarlyMinWords {
		return true
	}
	if hasSemanticTerminalCue(normalized) && words >= brainPrefetchMinWords {
		return true
	}
	if utteranceAge >= brainPrefetchEarlyAge && words >= brainPrefetchMinWords+1 {
		return true
	}
	return false
}

func wordsInCanonical(canonical string) int {
	canonical = strings.TrimSpace(canonical)
	if canonical == "" {
		return 0
	}
	words := 1
	for i := 0; i < len(canonical); i++ {
		if canonical[i] == ' ' {
			words++
		}
	}
	return words
}

func thinkingDeltaPreview(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	preview := strings.TrimSpace(stripAssistantLeadPreamble(raw))
	if preview == "" {
		preview = raw
	}
	preview = strings.Join(strings.Fields(preview), " ")
	if preview == "" {
		return ""
	}

	canon := canonicalizeForLeadFiller(preview)
	if canon == "" {
		return ""
	}
	if isAssistantLeadAckPrefix(canon) || isAssistantLeadFillerPrefix(canon) {
		return ""
	}

	runes := []rune(preview)
	if len(runes) <= thinkingDeltaPreviewMaxRunes {
		return preview
	}
	cut := string(runes[:thinkingDeltaPreviewMaxRunes])
	if idx := strings.LastIndex(cut, " "); idx >= 24 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut) + "..."
}
