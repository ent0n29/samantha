package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ent0n29/samantha/internal/audio"
)

type LocalConfig struct {
	WhisperCLI       string
	WhisperModelPath string
	WhisperLanguage  string
	WhisperThreads   int
	WhisperBeamSize  int
	WhisperBestOf    int
	STTProfile       string
	// STTAutoDownloadMissing attempts to fetch the selected model when it is not present.
	STTAutoDownloadMissing bool

	KokoroPython       string
	KokoroWorkerScript string
	KokoroVoice        string
	KokoroLangCode     string
}

type whisperTranscriber interface {
	Transcribe(ctx context.Context, pcm16le []byte, sampleRate int) (string, error)
}

type LocalProvider struct {
	cfg           LocalConfig
	whisper       whisperTranscriber
	whisperServer *whisperServer
	kokoroTTS     *kokoroWorker
}

func downloadWhisperModelIfMissing(modelPath string) error {
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return fmt.Errorf("empty model path")
	}
	if _, err := os.Stat(modelPath); err == nil {
		return nil
	}
	filename := strings.TrimSpace(filepath.Base(modelPath))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		return fmt.Errorf("invalid model filename: %q", filename)
	}
	if !strings.HasPrefix(filename, "ggml-") || !strings.HasSuffix(filename, ".bin") {
		return fmt.Errorf("unsupported model filename %q; expected whisper.cpp ggml model", filename)
	}

	modelDir := filepath.Dir(modelPath)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return err
	}

	url := "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + filename
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	tmpPath := modelPath + ".download"
	if err := os.RemoveAll(tmpPath); err != nil {
		return err
	}
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if n <= 0 {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("downloaded empty model payload")
	}

	if err := os.Rename(tmpPath, modelPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func NewLocalProvider(cfg LocalConfig) (*LocalProvider, error) {
	modelPath := strings.TrimSpace(cfg.WhisperModelPath)
	if modelPath == "" {
		return nil, fmt.Errorf("LOCAL_WHISPER_MODEL_PATH is required")
	}
	if !filepath.IsAbs(modelPath) {
		if wd, err := os.Getwd(); err == nil {
			modelPath = filepath.Join(wd, modelPath)
		}
	}
	if _, err := os.Stat(modelPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) && cfg.STTAutoDownloadMissing {
			if dlErr := downloadWhisperModelIfMissing(modelPath); dlErr != nil {
				return nil, fmt.Errorf("whisper.cpp model not found: %s (auto-download failed: %v)", modelPath, dlErr)
			}
		} else {
			return nil, fmt.Errorf("whisper.cpp model not found: %s", modelPath)
		}
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("whisper.cpp model not found: %s", modelPath)
	}

	language := strings.TrimSpace(cfg.WhisperLanguage)
	if language == "" {
		language = "en"
	}

	threads := cfg.WhisperThreads
	if threads < 0 {
		return nil, fmt.Errorf("LOCAL_WHISPER_THREADS must be >= 0")
	}
	if threads == 0 {
		// Auto-pick: bias for realtime responsiveness on Apple Silicon.
		threads = 4
		if n := runtime.NumCPU(); n > 0 {
			threads = n
		}
		if threads > 8 {
			threads = 8
		}
		if threads < 2 {
			threads = 2
		}
	}

	beamSize := cfg.WhisperBeamSize
	if beamSize <= 0 {
		beamSize = 1
	}
	bestOf := cfg.WhisperBestOf
	if bestOf <= 0 {
		bestOf = 1
	}

	var (
		transcriber whisperTranscriber
		ws          *whisperServer
	)
	if srv, err := startWhisperServer(modelPath, language, threads, beamSize, bestOf); err == nil {
		transcriber = srv
		ws = srv
	} else {
		cli, err := newWhisperCPP(cfg.WhisperCLI, modelPath, language, threads, beamSize, bestOf)
		if err != nil {
			return nil, err
		}
		transcriber = cli
	}

	py := strings.TrimSpace(cfg.KokoroPython)
	if py == "" {
		// Prefer a local venv if present.
		for _, candidate := range []string{".venv/bin/python3", ".venv/bin/python", "python3"} {
			if p, err := exec.LookPath(candidate); err == nil && strings.TrimSpace(p) != "" {
				py = p
				break
			}
		}
	}
	if strings.TrimSpace(py) == "" {
		return nil, fmt.Errorf("LOCAL_KOKORO_PYTHON not set and python3 not found on PATH")
	}

	script := strings.TrimSpace(cfg.KokoroWorkerScript)
	if script == "" {
		script = "scripts/kokoro_worker.py"
	}
	if !filepath.IsAbs(script) {
		if wd, err := os.Getwd(); err == nil {
			script = filepath.Join(wd, script)
		}
	}
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("kokoro worker script not found: %s", script)
	}

	worker, err := startKokoroWorker(py, script, strings.TrimSpace(cfg.KokoroLangCode))
	if err != nil {
		return nil, err
	}

	return &LocalProvider{
		cfg:           cfg,
		whisper:       transcriber,
		whisperServer: ws,
		kokoroTTS:     worker,
	}, nil
}

func (p *LocalProvider) Close() error {
	var errs []string
	if p.whisperServer != nil {
		if err := p.whisperServer.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("whisper-server: %v", err))
		}
	}
	if p.kokoroTTS != nil {
		if err := p.kokoroTTS.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("kokoro: %v", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (p *LocalProvider) STTBackend() string {
	if p != nil && p.whisperServer != nil {
		return "whisper-server"
	}
	return "whisper-cli"
}

func (p *LocalProvider) StartSession(ctx context.Context, sessionID string) (STTSession, <-chan STTEvent, error) {
	events := make(chan STTEvent, 256)
	baseCtx, cancel := context.WithCancel(ctx)
	s := &localSTTSession{
		whisper:    p.whisper,
		events:     events,
		sessionID:  sessionID,
		baseCtx:    baseCtx,
		baseCancel: cancel,
		workCh:     make(chan sttWork, 1),
		workerDone: make(chan struct{}),
		partialCfg: localSTTPartialConfigForProfile(p.cfg.STTProfile),
	}
	go s.worker()
	return s, events, nil
}

func (p *LocalProvider) StartStream(_ context.Context, voiceID, _ string, settings TTSSettings) (TTSStream, error) {
	voiceID = strings.TrimSpace(voiceID)
	if voiceID == "" {
		voiceID = strings.TrimSpace(p.cfg.KokoroVoice)
	}
	if voiceID == "" {
		voiceID = "af_heart"
	}
	lang := strings.TrimSpace(p.cfg.KokoroLangCode)
	if lang == "" {
		lang = "a"
	}
	events := make(chan TTSEvent, 64)
	ctx, cancel := context.WithCancel(context.Background())
	// NOTE: We intentionally detach from the caller ctx here because some providers (Kokoro worker)
	// cannot be interrupted mid-synthesis without desynchronizing the JSON protocol. We still
	// stop emitting events on Close()/cancel.
	s := &localTTSStream{
		worker:   p.kokoroTTS,
		events:   events,
		voiceID:  voiceID,
		langCode: lang,
		settings: settings,
		ctx:      ctx,
		cancel:   cancel,
		segCh:    make(chan string, 16),
		done:     make(chan struct{}),
	}
	go s.synthLoop()
	return s, nil
}

type localSTTSession struct {
	whisper   whisperTranscriber
	events    chan STTEvent
	sessionID string

	mu     sync.Mutex
	pcm    []byte
	closed bool

	baseCtx    context.Context
	baseCancel context.CancelFunc

	workCh     chan sttWork
	workerDone chan struct{}

	activeCancel context.CancelFunc
	activeToken  int64

	partialCfg           localSTTPartialConfig
	partialCancel        context.CancelFunc
	partialToken         int64
	partialLastStartedAt time.Time
	partialLastText      string
	partialLastAt        time.Time
	partialWG            sync.WaitGroup
}

type localSTTPartialConfig struct {
	Enabled     bool
	MinInterval time.Duration
	MinAudio    time.Duration
	MaxTail     time.Duration
	Timeout     time.Duration
}

type localSTTPartialJob struct {
	token      int64
	ctx        context.Context
	cancel     context.CancelFunc
	pcm        []byte
	sampleRate int
}

const (
	localSTTPartialMinDeltaRunes           = 2
	localSTTCommitFromPartialFreshWindow   = 1200 * time.Millisecond
	localSTTCommitFromPartialMinAudio      = 500 * time.Millisecond
	localSTTCommitFromPartialMaxAudio      = 3200 * time.Millisecond
	localSTTCommitFromTerminalMinAudio     = 320 * time.Millisecond
	localSTTCommitFromTerminalMinWordCount = 2
	localSTTCommitFromDefaultMinWordCount  = 3
)

func localSTTPartialConfigForProfile(profile string) localSTTPartialConfig {
	cfg := localSTTPartialConfig{
		Enabled:     true,
		MinInterval: 260 * time.Millisecond,
		MinAudio:    260 * time.Millisecond,
		MaxTail:     2800 * time.Millisecond,
		Timeout:     5 * time.Second,
	}
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "fast":
		cfg.MinInterval = 170 * time.Millisecond
		cfg.MinAudio = 180 * time.Millisecond
		cfg.MaxTail = 2 * time.Second
		cfg.Timeout = 3500 * time.Millisecond
	case "accurate":
		cfg.MinInterval = 750 * time.Millisecond
		cfg.MinAudio = 950 * time.Millisecond
		cfg.MaxTail = 7 * time.Second
		cfg.Timeout = 10 * time.Second
	}
	return cfg
}

func (s *localSTTSession) SendAudioChunk(_ context.Context, audioBase64 string, sampleRate int, commit bool) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if strings.TrimSpace(audioBase64) != "" {
		decoded, err := base64.StdEncoding.DecodeString(audioBase64)
		if err == nil && len(decoded) > 0 {
			s.pcm = append(s.pcm, decoded...)

			// Safety: cap uncommitted audio so a hot mic can't grow memory unbounded if commit never arrives.
			const maxUncommittedSeconds = 60
			maxBytes := sampleRate * 2 * maxUncommittedSeconds
			if maxBytes > 0 && len(s.pcm) > maxBytes {
				s.pcm = s.pcm[len(s.pcm)-maxBytes:]
				// If we trimmed a very large backing array, copy to a smaller one to allow GC.
				if cap(s.pcm) > maxBytes*4 {
					trimmed := make([]byte, len(s.pcm))
					copy(trimmed, s.pcm)
					s.pcm = trimmed
				}
			}
		}
	}

	var partialJob *localSTTPartialJob
	if !commit {
		partialJob = s.nextPartialJobLocked(sampleRate)
		s.mu.Unlock()
		if partialJob != nil {
			s.startPartialTranscribe(partialJob)
		}
		return nil
	}

	pcm := make([]byte, len(s.pcm))
	copy(pcm, s.pcm)
	s.pcm = s.pcm[:0]
	partialText := strings.TrimSpace(s.partialLastText)
	partialAt := s.partialLastAt
	// Prefer "latest wins": cancel any in-flight transcription so we stay responsive.
	if s.activeCancel != nil {
		s.activeCancel()
		s.activeCancel = nil
	}
	if s.partialCancel != nil {
		s.partialCancel()
		s.partialCancel = nil
	}
	// Commit starts a new utterance lifecycle; invalidate stale partial state.
	s.partialToken++
	s.partialLastStartedAt = time.Time{}
	s.partialLastText = ""
	s.partialLastAt = time.Time{}
	s.mu.Unlock()

	if len(pcm) == 0 {
		return nil
	}

	if shouldUseLocalPartialAsCommit(partialText, partialAt, len(pcm), sampleRate) {
		select {
		case s.events <- STTEvent{
			Type:       STTEventCommitted,
			Text:       partialText,
			Confidence: 0.66,
			Source:     "partial_commit",
			Timestamp:  time.Now().UnixMilli(),
		}:
		default:
		}
		return nil
	}

	s.enqueueWork(sttWork{PCM: pcm, SampleRate: sampleRate})

	return nil
}

func (s *localSTTSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.activeCancel != nil {
		s.activeCancel()
		s.activeCancel = nil
	}
	if s.partialCancel != nil {
		s.partialCancel()
		s.partialCancel = nil
	}
	cancel := s.baseCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-s.workerDone
	s.partialWG.Wait()
	return nil
}

type sttWork struct {
	PCM        []byte
	SampleRate int
}

func (s *localSTTSession) enqueueWork(w sttWork) {
	select {
	case s.workCh <- w:
		return
	default:
		// Drop any pending work and replace it with the most recent commit.
		select {
		case <-s.workCh:
		default:
		}
		select {
		case s.workCh <- w:
		default:
		}
	}
}

func (s *localSTTSession) nextPartialJobLocked(sampleRate int) *localSTTPartialJob {
	cfg := s.partialCfg
	if !cfg.Enabled || s.whisper == nil {
		return nil
	}
	if s.partialCancel != nil {
		return nil
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	now := time.Now()
	if !s.partialLastStartedAt.IsZero() && now.Sub(s.partialLastStartedAt) < cfg.MinInterval {
		return nil
	}
	minBytes := bytesForAudioDuration(sampleRate, cfg.MinAudio)
	if minBytes <= 0 {
		minBytes = sampleRate * 2
	}
	if len(s.pcm) < minBytes {
		return nil
	}

	tailBytes := bytesForAudioDuration(sampleRate, cfg.MaxTail)
	if tailBytes <= 0 || tailBytes > len(s.pcm) {
		tailBytes = len(s.pcm)
	}
	start := len(s.pcm) - tailBytes
	pcm := make([]byte, tailBytes)
	copy(pcm, s.pcm[start:])

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(s.baseCtx, timeout)
	s.partialToken++
	token := s.partialToken
	s.partialCancel = cancel
	s.partialLastStartedAt = now
	return &localSTTPartialJob{
		token:      token,
		ctx:        ctx,
		cancel:     cancel,
		pcm:        pcm,
		sampleRate: sampleRate,
	}
}

func (s *localSTTSession) startPartialTranscribe(job *localSTTPartialJob) {
	if job == nil || len(job.pcm) == 0 {
		return
	}
	s.partialWG.Add(1)
	go func(j localSTTPartialJob) {
		defer s.partialWG.Done()
		text, err := s.whisper.Transcribe(j.ctx, j.pcm, j.sampleRate)
		j.cancel()

		s.mu.Lock()
		if s.partialToken == j.token {
			s.partialCancel = nil
		}
		closed := s.closed
		if closed {
			s.mu.Unlock()
			return
		}
		if err != nil {
			s.mu.Unlock()
			// Best-effort preview only: commit STT path remains authoritative.
			return
		}
		text = normalizeLocalPartialText(text)
		if !shouldEmitLocalPartialUpdate(s.partialLastText, text) {
			s.mu.Unlock()
			return
		}
		s.partialLastText = text
		s.partialLastAt = time.Now()
		s.mu.Unlock()

		select {
		case s.events <- STTEvent{
			Type:       STTEventPartial,
			Text:       text,
			Confidence: 0.62,
			Timestamp:  time.Now().UnixMilli(),
		}:
		default:
		}
	}(*job)
}

func normalizeLocalPartialText(raw string) string {
	return strings.TrimSpace(raw)
}

func shouldEmitLocalPartialUpdate(prev, next string) bool {
	prev = normalizeLocalPartialText(prev)
	next = normalizeLocalPartialText(next)
	if next == "" {
		return false
	}
	if prev == "" {
		return true
	}
	if prev == next {
		return false
	}
	if strings.HasPrefix(prev, next) {
		return false
	}
	if strings.HasPrefix(next, prev) {
		delta := utf8.RuneCountInString(next) - utf8.RuneCountInString(prev)
		if delta < localSTTPartialMinDeltaRunes {
			return false
		}
	}
	return true
}

func bytesForAudioDuration(sampleRate int, d time.Duration) int {
	if d <= 0 {
		return 0
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	seconds := d.Seconds()
	if seconds <= 0 {
		return 0
	}
	return int(float64(sampleRate*2) * seconds)
}

func shouldUseLocalPartialAsCommit(partialText string, partialAt time.Time, audioBytes int, sampleRate int) bool {
	partialText = strings.TrimSpace(partialText)
	if partialText == "" || partialAt.IsZero() {
		return false
	}
	if time.Since(partialAt) > localSTTCommitFromPartialFreshWindow {
		return false
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	normalized := normalizeSemanticEndpointText(partialText)
	if normalized == "" {
		return false
	}
	terminal := hasSemanticTerminalCue(normalized)
	minAudio := localSTTCommitFromPartialMinAudio
	if terminal {
		minAudio = localSTTCommitFromTerminalMinAudio
	}
	minAudioBytes := bytesForAudioDuration(sampleRate, minAudio)
	if audioBytes < minAudioBytes {
		return false
	}
	maxAudioBytes := bytesForAudioDuration(sampleRate, localSTTCommitFromPartialMaxAudio)
	if maxAudioBytes > 0 && audioBytes > maxAudioBytes {
		return false
	}
	leadCanon := canonicalizeForLeadFiller(partialText)
	if isAssistantLeadFillerPrefix(leadCanon) || hasAssistantLeadFillerPhrase(leadCanon) {
		return false
	}
	wordCount := 1 + strings.Count(normalized, " ")
	// Avoid committing clearly unfinished clauses from a partial transcript.
	if hasSemanticContinuationCue(normalized) && !hasSemanticTerminalCue(normalized) {
		return false
	}
	if wordCount < localSTTCommitFromDefaultMinWordCount {
		if !(terminal && wordCount >= localSTTCommitFromTerminalMinWordCount) {
			return false
		}
	}
	return true
}

func hasAssistantLeadFillerPhrase(canon string) bool {
	canon = strings.TrimSpace(canon)
	if canon == "" {
		return false
	}
	for _, phrase := range assistantLeadFillerPhrases {
		if canon == phrase || strings.HasPrefix(canon, phrase+" ") {
			return true
		}
	}
	return false
}

func (s *localSTTSession) worker() {
	defer close(s.workerDone)
	defer close(s.events)

	for {
		select {
		case <-s.baseCtx.Done():
			return
		case w := <-s.workCh:
			if len(w.PCM) == 0 {
				continue
			}

			ctx, cancel := context.WithTimeout(s.baseCtx, 25*time.Second)
			s.mu.Lock()
			s.activeToken++
			token := s.activeToken
			s.activeCancel = cancel
			s.mu.Unlock()

			text, err := s.whisper.Transcribe(ctx, w.PCM, w.SampleRate)
			cancel()

			s.mu.Lock()
			if s.activeToken == token {
				s.activeCancel = nil
			}
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}

			if err != nil {
				if errors.Is(err, context.Canceled) {
					// Expected when a newer commit arrives; keep the UI quiet.
					continue
				}
				select {
				case s.events <- STTEvent{
					Type:      STTEventError,
					Code:      "local_stt_failed",
					Detail:    err.Error(),
					Retryable: false,
					Timestamp: time.Now().UnixMilli(),
				}:
				default:
				}
				continue
			}

			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			select {
			case s.events <- STTEvent{
				Type:       STTEventCommitted,
				Text:       text,
				Confidence: 0.8,
				Source:     "full_commit",
				Timestamp:  time.Now().UnixMilli(),
			}:
			default:
			}
		}
	}
}

type localTTSStream struct {
	worker   *kokoroWorker
	events   chan TTSEvent
	voiceID  string
	langCode string
	settings TTSSettings

	ctx    context.Context
	cancel context.CancelFunc

	mu               sync.Mutex
	pending          string
	segCh            chan string
	segChClosed      bool
	closed           bool
	segmentsOut      int
	firstChunkQueued bool

	done chan struct{}
}

func (s *localTTSStream) SendText(_ context.Context, text string, tryTrigger bool) error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}

	var ready []string
	s.mu.Lock()
	if !s.closed {
		s.pending += text
		if tryTrigger {
			minChars, maxChars := localTTSSegmentBounds(s.firstChunkQueued, s.settings.Speed)
			ready, s.pending = splitTTSReadySegments(s.pending, minChars, maxChars)
			if len(ready) > 0 {
				s.firstChunkQueued = true
			}
		}
	}
	s.mu.Unlock()

	for _, seg := range ready {
		s.enqueueSegment(seg)
	}
	return nil
}

func (s *localTTSStream) CloseInput(ctx context.Context) error {
	_ = ctx // the stream owns cancellation via Close()
	var final string
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	// CloseInput means no more text will be appended, but we still want to flush any
	// pending trailing segment before shutting down the synth loop.
	s.closed = true
	final = strings.TrimSpace(s.pending)
	s.pending = ""
	s.mu.Unlock()

	if final != "" {
		s.enqueueSegment(final)
	}

	s.mu.Lock()
	if !s.segChClosed {
		close(s.segCh)
		s.segChClosed = true
	}
	s.mu.Unlock()
	return nil
}

func (s *localTTSStream) Events() <-chan TTSEvent { return s.events }

func (s *localTTSStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if !s.segChClosed {
		close(s.segCh)
		s.segChClosed = true
	}
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	<-s.done
	return nil
}

func (s *localTTSStream) enqueueSegment(text string) {
	seg := strings.TrimSpace(text)
	if seg == "" {
		return
	}
	// Best-effort safety: if the stream was closed concurrently, sending would panic.
	defer func() { _ = recover() }()
	select {
	case s.segCh <- seg:
	default:
		// Backpressure: block rather than dropping audio segments.
		s.segCh <- seg
	}
}

func (s *localTTSStream) synthLoop() {
	defer close(s.done)
	defer close(s.events)

	for seg := range s.segCh {
		if s.ctx.Err() != nil {
			return
		}
		audio, format, err := s.worker.Synthesize(s.ctx, kokoroRequest{
			Text:     seg,
			Voice:    s.voiceID,
			LangCode: s.langCode,
			Speed:    s.settings.Speed,
		})
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			select {
			case s.events <- TTSEvent{Type: TTSEventError, Code: "local_tts_failed", Detail: err.Error(), Retryable: false}:
			default:
			}
			return
		}
		if len(audio) == 0 {
			continue
		}
		s.mu.Lock()
		s.segmentsOut++
		s.mu.Unlock()
		select {
		case s.events <- TTSEvent{Type: TTSEventAudio, AudioBase64: base64.StdEncoding.EncodeToString(audio), Format: format}:
		default:
		}
	}

	select {
	case s.events <- TTSEvent{Type: TTSEventFinal}:
	default:
	}
}

func splitTTSReadySegments(text string, minChars, maxChars int) ([]string, string) {
	if minChars <= 0 {
		minChars = 28
	}
	if maxChars <= 0 {
		maxChars = 200
	}
	s := strings.TrimLeft(text, " \t\r\n")
	if s == "" {
		return nil, ""
	}

	ready := make([]string, 0, 2)
	rest := s

	for {
		if len(rest) < minChars {
			return ready, rest
		}

		limit := len(rest)
		if limit > maxChars {
			limit = maxChars
		}

		cut := -1
		// Prefer sentence/paragraph boundaries.
		if idx := strings.LastIndexAny(rest[:limit], ".?!\n"); idx >= 0 {
			if idx+1 >= minChars {
				cut = idx + 1
			}
		}
		// If we are getting too long without a boundary, split near whitespace.
		if cut < 0 && len(rest) > maxChars {
			if ws := strings.LastIndexAny(rest[:limit], " \t\n"); ws >= 0 {
				if ws >= minChars {
					cut = ws
				}
			}
			if cut < 0 {
				cut = limit
			}
		}

		// Keep first-audio latency low, but avoid over-fragmenting speech into very
		// short chunks that sound robotic.
		fallbackOffset := 56
		if minChars > 56 {
			fallbackOffset = (minChars / 2) + 28
			if fallbackOffset > 110 {
				fallbackOffset = 110
			}
		}
		if cut < 0 && len(rest) >= minChars+fallbackOffset {
			fallbackWindow := 80
			if minChars > 56 {
				fallbackWindow = (minChars / 2) + 64
				if fallbackWindow > 160 {
					fallbackWindow = 160
				}
			}
			fallbackLimit := minChars + fallbackWindow
			if fallbackLimit > len(rest) {
				fallbackLimit = len(rest)
			}
			if ws := strings.IndexAny(rest[minChars:fallbackLimit], " \t\n"); ws >= 0 {
				cut = minChars + ws
			} else {
				cut = minChars
			}
		}

		if cut < 0 {
			return ready, rest
		}

		seg := strings.TrimSpace(rest[:cut])
		rest = strings.TrimLeft(rest[cut:], " \t\r\n")
		if seg != "" {
			ready = append(ready, seg)
		}
		if rest == "" {
			return ready, ""
		}
	}
}

func localTTSSegmentBounds(firstChunkQueued bool, speed float64) (minChars, maxChars int) {
	if !firstChunkQueued {
		// First chunk prioritizes quick first audio.
		return 24, 220
	}

	// Subsequent chunks prioritize continuity to avoid robotic start/stop prosody.
	minChars = 72
	maxChars = 320

	switch {
	case speed >= 1.04:
		// Faster speech can use slightly shorter chunks without sounding clipped.
		minChars -= 10
		maxChars -= 20
	case speed > 0 && speed <= 0.9:
		// Slower speech benefits from longer clauses.
		minChars += 10
		maxChars += 20
	}

	if minChars < 56 {
		minChars = 56
	}
	if minChars > 110 {
		minChars = 110
	}
	if maxChars < minChars+120 {
		maxChars = minChars + 120
	}
	if maxChars > 380 {
		maxChars = 380
	}
	return minChars, maxChars
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailBuffer(max int) *tailBuffer {
	if max <= 0 {
		max = 16 << 10
	}
	return &tailBuffer{max: max}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

type whisperServer struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	baseURL string
	client  *http.Client
	logTail *tailBuffer
	closed  bool
}

func startWhisperServer(modelPath, language string, threads, beamSize, bestOf int) (*whisperServer, error) {
	path, err := exec.LookPath("whisper-server")
	if err != nil {
		return nil, err
	}

	port, err := pickFreePort()
	if err != nil {
		return nil, err
	}

	args := []string{
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-m", modelPath,
		"-l", language,
		"-nt",
	}
	if threads > 0 {
		args = append(args, "-t", strconv.Itoa(threads))
	}
	if beamSize > 0 {
		args = append(args, "-bs", strconv.Itoa(beamSize))
	}
	if bestOf > 0 {
		args = append(args, "-bo", strconv.Itoa(bestOf))
	}

	tail := newTailBuffer(24 << 10)
	cmd := exec.Command(path, args...)
	injectWhisperLibraryEnv(cmd, path)
	cmd.Stdout = tail
	cmd.Stderr = tail

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{}

	// Wait until the server is reachable.
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/", nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return &whisperServer{
					cmd:     cmd,
					baseURL: baseURL,
					client:  client,
					logTail: tail,
				}, nil
			}
		}
		time.Sleep(80 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	msg := tail.String()
	if msg == "" {
		msg = "whisper-server did not become ready"
	}
	return nil, fmt.Errorf("%s", msg)
}

func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok || addr == nil || addr.Port == 0 {
		return 0, fmt.Errorf("failed to allocate port")
	}
	return addr.Port, nil
}

func injectWhisperLibraryEnv(cmd *exec.Cmd, toolPath string) {
	if cmd == nil {
		return
	}
	toolPath = strings.TrimSpace(toolPath)
	if toolPath == "" {
		return
	}

	toolDir := filepath.Dir(toolPath)
	candidates := []string{
		filepath.Clean(filepath.Join(toolDir, "..", "lib")),
		filepath.Clean(filepath.Join(toolDir, "lib")),
	}
	libDir := ""
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			libDir = candidate
			break
		}
	}
	if libDir == "" {
		return
	}

	env := cmd.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	env = prependPathEnv(env, "DYLD_FALLBACK_LIBRARY_PATH", libDir)
	env = prependPathEnv(env, "DYLD_LIBRARY_PATH", libDir)
	cmd.Env = env
}

func prependPathEnv(env []string, key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return env
	}
	prefix := key + "="
	for i := range env {
		if !strings.HasPrefix(env[i], prefix) {
			continue
		}
		current := strings.TrimPrefix(env[i], prefix)
		if pathListContains(current, value) {
			return env
		}
		if strings.TrimSpace(current) == "" {
			env[i] = prefix + value
		} else {
			env[i] = prefix + value + ":" + current
		}
		return env
	}
	return append(env, prefix+value)
}

func pathListContains(pathList, value string) bool {
	value = filepath.Clean(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, item := range strings.Split(pathList, ":") {
		if filepath.Clean(strings.TrimSpace(item)) == value {
			return true
		}
	}
	return false
}

func (s *whisperServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cmd := s.cmd
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Best-effort graceful shutdown.
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-time.After(1200 * time.Millisecond):
		_ = cmd.Process.Kill()
		<-done
	case <-done:
	}
	return nil
}

func (s *whisperServer) Transcribe(ctx context.Context, pcm16le []byte, sampleRate int) (string, error) {
	if len(pcm16le) == 0 {
		return "", nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	wav, err := audio.EncodeWAVPCM16LE(pcm16le, sampleRate)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return "", fmt.Errorf("whisper-server closed")
	}

	// Serialize requests; the server is typically configured with a single processor.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", fmt.Errorf("whisper-server closed")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "audio.wav")
	if err != nil {
		_ = mw.Close()
		return "", err
	}
	if _, err := fw.Write(wav); err != nil {
		_ = mw.Close()
		return "", err
	}
	_ = mw.WriteField("temperature", "0.0")
	_ = mw.WriteField("response_format", "json")
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/inference", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper-server HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

type whisperCPP struct {
	cliPath   string
	modelPath string
	language  string
	threads   int
	beamSize  int
	bestOf    int
}

func newWhisperCPP(cli, modelPath, language string, threads, beamSize, bestOf int) (whisperCPP, error) {
	cli = strings.TrimSpace(cli)
	if cli == "" {
		cli = "whisper-cli"
	}
	cliPath, err := exec.LookPath(cli)
	if err != nil {
		return whisperCPP{}, fmt.Errorf("whisper.cpp CLI not found (%s). Run `make setup-local-voice`", cli)
	}
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return whisperCPP{}, fmt.Errorf("LOCAL_WHISPER_MODEL_PATH is required")
	}
	if !filepath.IsAbs(modelPath) {
		if wd, err := os.Getwd(); err == nil {
			modelPath = filepath.Join(wd, modelPath)
		}
	}
	if _, err := os.Stat(modelPath); err != nil {
		return whisperCPP{}, fmt.Errorf("whisper.cpp model not found: %s", modelPath)
	}
	language = strings.TrimSpace(language)
	if language == "" {
		language = "en"
	}

	if threads < 0 {
		return whisperCPP{}, fmt.Errorf("LOCAL_WHISPER_THREADS must be >= 0")
	}
	if threads == 0 {
		// Auto-pick: bias for realtime responsiveness on Apple Silicon.
		threads = 4
		if n := runtime.NumCPU(); n > 0 {
			threads = n
		}
		if threads > 8 {
			threads = 8
		}
		if threads < 2 {
			threads = 2
		}
	}

	if beamSize <= 0 {
		beamSize = 1
	}
	if bestOf <= 0 {
		bestOf = 1
	}

	return whisperCPP{
		cliPath:   cliPath,
		modelPath: modelPath,
		language:  language,
		threads:   threads,
		beamSize:  beamSize,
		bestOf:    bestOf,
	}, nil
}

func (w whisperCPP) Transcribe(ctx context.Context, pcm16le []byte, sampleRate int) (string, error) {
	if len(pcm16le) == 0 {
		return "", nil
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	tmpDir, err := os.MkdirTemp("", "samantha-whisper-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	wavPath := filepath.Join(tmpDir, "audio.wav")
	if err := audio.WriteWAVPCM16LEFile(wavPath, pcm16le, sampleRate); err != nil {
		return "", err
	}
	outPrefix := filepath.Join(tmpDir, "out")

	// whisper.cpp CLI flag set varies slightly across builds; keep this conservative.
	args := []string{
		"-m", w.modelPath,
		"-f", wavPath,
		"-l", w.language,
		"-otxt",
		"-of", outPrefix,
		"-nt",
	}
	if w.threads > 0 {
		args = append(args, "-t", strconv.Itoa(w.threads))
	}
	if w.beamSize > 0 {
		args = append(args, "-bs", strconv.Itoa(w.beamSize))
	}
	if w.bestOf > 0 {
		args = append(args, "-bo", strconv.Itoa(w.bestOf))
	}

	cmd := exec.CommandContext(ctx, w.cliPath, args...)
	injectWhisperLibraryEnv(cmd, w.cliPath)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		detail := strings.TrimSpace(stderr.String())
		// whisper.cpp can be extremely chatty; keep errors readable.
		if len(detail) > 8<<10 {
			detail = detail[len(detail)-(8<<10):]
			detail = strings.TrimSpace(detail)
		}
		if detail == "" {
			detail = err.Error()
		}
		// Timeouts are expected when using heavy models; give a clear hint.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("whisper.cpp timed out; use a smaller model (e.g. ggml-tiny.en.bin) or reduce utterance length")
		}
		return "", fmt.Errorf("whisper.cpp failed: %s", detail)
	}

	txtPath := outPrefix + ".txt"
	b, err := os.ReadFile(txtPath)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(b))
	return text, nil
}

type kokoroWorker struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	dec    *json.Decoder
	closed bool
}

type kokoroRequest struct {
	Text     string  `json:"text"`
	Voice    string  `json:"voice"`
	LangCode string  `json:"lang_code"`
	Speed    float64 `json:"speed"`
}

type kokoroResponse struct {
	ID          string `json:"id"`
	OK          bool   `json:"ok"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate"`
	AudioBase64 string `json:"audio_base64"`
	Error       string `json:"error"`
}

func startKokoroWorker(pythonPath, scriptPath, defaultLang string) (*kokoroWorker, error) {
	cmd := exec.Command(pythonPath, "-u", scriptPath)
	cmd.Env = append(os.Environ(), "PYTORCH_ENABLE_MPS_FALLBACK=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(stdout)
	w := &kokoroWorker{cmd: cmd, stdin: stdin, dec: dec}

	// Fire a cheap warmup request so dependency errors surface early.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if _, _, err := w.Synthesize(ctx, kokoroRequest{
		Text:     "warmup",
		Voice:    "af_heart",
		LangCode: strings.TrimSpace(defaultLang),
		Speed:    1.0,
	}); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("kokoro worker failed to start: %s", msg)
	}

	return w, nil
}

func (w *kokoroWorker) Synthesize(ctx context.Context, req kokoroRequest) ([]byte, string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, "", fmt.Errorf("kokoro worker closed")
	}

	type requestLine struct {
		ID       string  `json:"id"`
		Text     string  `json:"text"`
		Voice    string  `json:"voice"`
		LangCode string  `json:"lang_code"`
		Speed    float64 `json:"speed"`
	}
	id := fmt.Sprintf("req-%d", time.Now().UnixNano())
	line := requestLine{
		ID:       id,
		Text:     req.Text,
		Voice:    req.Voice,
		LangCode: req.LangCode,
		Speed:    req.Speed,
	}
	if strings.TrimSpace(line.LangCode) == "" {
		line.LangCode = "a"
	}
	if strings.TrimSpace(line.Voice) == "" {
		line.Voice = "af_heart"
	}
	if line.Speed <= 0 {
		line.Speed = 1.0
	}

	b, _ := json.Marshal(line)
	b = append(b, '\n')
	if _, err := w.stdin.Write(b); err != nil {
		return nil, "", err
	}

	// Decode exactly one response (worker is single-flight guarded by mu).
	var resp kokoroResponse
	if err := w.dec.Decode(&resp); err != nil {
		return nil, "", err
	}
	if resp.ID != id {
		return nil, "", fmt.Errorf("kokoro worker out-of-sync (got %q, expected %q)", resp.ID, id)
	}
	if !resp.OK {
		msg := strings.TrimSpace(resp.Error)
		if msg == "" {
			msg = "unknown kokoro error"
		}
		return nil, "", fmt.Errorf("%s", msg)
	}

	format := strings.TrimSpace(resp.Format)
	if format == "" {
		format = "wav_24000"
	}
	if strings.TrimSpace(resp.AudioBase64) == "" {
		return []byte{}, format, nil
	}

	audio, err := base64.StdEncoding.DecodeString(resp.AudioBase64)
	if err != nil {
		return nil, "", fmt.Errorf("decode audio_base64: %w", err)
	}
	return audio, format, nil
}

func (w *kokoroWorker) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	stdin := w.stdin
	cmd := w.cmd
	w.stdin = nil
	w.cmd = nil
	w.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-time.After(1200 * time.Millisecond):
		_ = cmd.Process.Kill()
		<-done
	case <-done:
	}
	return nil
}
