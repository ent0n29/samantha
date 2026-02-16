package voice

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedWhisperTranscriber struct {
	mu        sync.Mutex
	responses []string
	delay     time.Duration
	calls     int
}

func (s *scriptedWhisperTranscriber) Transcribe(ctx context.Context, _ []byte, _ int) (string, error) {
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.responses) == 0 {
		return "", nil
	}
	out := s.responses[0]
	if len(s.responses) > 1 {
		s.responses = s.responses[1:]
	}
	return out, nil
}

func (s *scriptedWhisperTranscriber) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestLocalSTTSessionEmitsPartialBeforeCommit(t *testing.T) {
	transcriber := &scriptedWhisperTranscriber{
		responses: []string{
			"let me think through",
			"let me think through the plan",
		},
	}
	s := newTestLocalSTTSession(t, transcriber)

	audio := zeroPCMBase64(bytesForAudioDuration(16000, 1300*time.Millisecond))
	if err := s.SendAudioChunk(context.Background(), audio, 16000, false); err != nil {
		t.Fatalf("SendAudioChunk(partial) error = %v", err)
	}
	partial := waitForSTTEventType(t, s.events, STTEventPartial, time.Second)
	if strings.TrimSpace(partial.Text) != "let me think through" {
		t.Fatalf("partial.Text = %q, want %q", partial.Text, "let me think through")
	}

	if err := s.SendAudioChunk(context.Background(), "", 16000, true); err != nil {
		t.Fatalf("SendAudioChunk(commit) error = %v", err)
	}
	committed := waitForSTTEventType(t, s.events, STTEventCommitted, 2*time.Second)
	if strings.TrimSpace(committed.Text) != "let me think through the plan" {
		t.Fatalf("committed.Text = %q, want %q", committed.Text, "let me think through the plan")
	}
	if committed.Source != "full_commit" {
		t.Fatalf("committed.Source = %q, want %q", committed.Source, "full_commit")
	}
}

func TestLocalSTTSessionSuppressesDuplicatePartials(t *testing.T) {
	transcriber := &scriptedWhisperTranscriber{
		responses: []string{
			"draft",
			"draft",
			"draft next",
		},
	}
	s := newTestLocalSTTSession(t, transcriber)
	audio := zeroPCMBase64(bytesForAudioDuration(16000, 900*time.Millisecond))

	if err := s.SendAudioChunk(context.Background(), audio, 16000, false); err != nil {
		t.Fatalf("SendAudioChunk(first partial) error = %v", err)
	}
	first := waitForSTTEventType(t, s.events, STTEventPartial, time.Second)
	if first.Text != "draft" {
		t.Fatalf("first partial = %q, want %q", first.Text, "draft")
	}

	if err := s.SendAudioChunk(context.Background(), audio, 16000, false); err != nil {
		t.Fatalf("SendAudioChunk(duplicate partial) error = %v", err)
	}
	ensureNoSTTEventType(t, s.events, STTEventPartial, 180*time.Millisecond)

	if err := s.SendAudioChunk(context.Background(), audio, 16000, false); err != nil {
		t.Fatalf("SendAudioChunk(next partial) error = %v", err)
	}
	second := waitForSTTEventType(t, s.events, STTEventPartial, time.Second)
	if second.Text != "draft next" {
		t.Fatalf("second partial = %q, want %q", second.Text, "draft next")
	}
}

func TestLocalSTTSessionCommitUsesFreshPartial(t *testing.T) {
	transcriber := &scriptedWhisperTranscriber{
		responses: []string{
			"we can ship this today",
		},
	}
	s := newTestLocalSTTSession(t, transcriber)
	audio := zeroPCMBase64(bytesForAudioDuration(16000, 1200*time.Millisecond))

	if err := s.SendAudioChunk(context.Background(), audio, 16000, false); err != nil {
		t.Fatalf("SendAudioChunk(partial) error = %v", err)
	}
	_ = waitForSTTEventType(t, s.events, STTEventPartial, time.Second)

	if err := s.SendAudioChunk(context.Background(), "", 16000, true); err != nil {
		t.Fatalf("SendAudioChunk(commit) error = %v", err)
	}
	committed := waitForSTTEventType(t, s.events, STTEventCommitted, time.Second)
	if strings.TrimSpace(committed.Text) != "we can ship this today" {
		t.Fatalf("committed.Text = %q, want %q", committed.Text, "we can ship this today")
	}
	if committed.Source != "partial_commit" {
		t.Fatalf("committed.Source = %q, want %q", committed.Source, "partial_commit")
	}
	if transcriber.Calls() != 1 {
		t.Fatalf("Transcribe calls = %d, want 1 (partial reused for commit)", transcriber.Calls())
	}
}

func TestLocalSTTSessionCommitDoesNotUseContinuationPartial(t *testing.T) {
	transcriber := &scriptedWhisperTranscriber{
		responses: []string{
			"and then we can",
			"and then we can ship this today",
		},
	}
	s := newTestLocalSTTSession(t, transcriber)
	audio := zeroPCMBase64(bytesForAudioDuration(16000, 1200*time.Millisecond))

	if err := s.SendAudioChunk(context.Background(), audio, 16000, false); err != nil {
		t.Fatalf("SendAudioChunk(partial) error = %v", err)
	}
	partial := waitForSTTEventType(t, s.events, STTEventPartial, time.Second)
	if strings.TrimSpace(partial.Text) != "and then we can" {
		t.Fatalf("partial.Text = %q, want %q", partial.Text, "and then we can")
	}

	if err := s.SendAudioChunk(context.Background(), "", 16000, true); err != nil {
		t.Fatalf("SendAudioChunk(commit) error = %v", err)
	}
	committed := waitForSTTEventType(t, s.events, STTEventCommitted, 2*time.Second)
	if strings.TrimSpace(committed.Text) != "and then we can ship this today" {
		t.Fatalf("committed.Text = %q, want %q", committed.Text, "and then we can ship this today")
	}
	if committed.Source != "full_commit" {
		t.Fatalf("committed.Source = %q, want %q", committed.Source, "full_commit")
	}
	if transcriber.Calls() != 2 {
		t.Fatalf("Transcribe calls = %d, want 2 (full commit transcription expected)", transcriber.Calls())
	}
}

func TestShouldEmitLocalPartialUpdate(t *testing.T) {
	cases := []struct {
		name string
		prev string
		next string
		want bool
	}{
		{name: "first", prev: "", next: "hello", want: true},
		{name: "same", prev: "hello", next: "hello", want: false},
		{name: "short extension", prev: "hello", next: "hello!", want: false},
		{name: "long extension", prev: "hello", next: "hello there", want: true},
		{name: "regression", prev: "hello there", next: "hello", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldEmitLocalPartialUpdate(tc.prev, tc.next); got != tc.want {
				t.Fatalf("shouldEmitLocalPartialUpdate(%q, %q) = %v, want %v", tc.prev, tc.next, got, tc.want)
			}
		})
	}
}

func TestShouldUseLocalPartialAsCommit(t *testing.T) {
	now := time.Now()
	if !shouldUseLocalPartialAsCommit("we can ship this today", now, bytesForAudioDuration(16000, time.Second), 16000) {
		t.Fatalf("expected fresh complete partial to be used as commit")
	}
	if shouldUseLocalPartialAsCommit("and then we can", now, bytesForAudioDuration(16000, time.Second), 16000) {
		t.Fatalf("expected continuation-style partial to require full commit transcription")
	}
	if shouldUseLocalPartialAsCommit("ship", now, bytesForAudioDuration(16000, time.Second), 16000) {
		t.Fatalf("expected too-short partial to be ignored")
	}
	if shouldUseLocalPartialAsCommit("we can ship this today", now.Add(-3*time.Second), bytesForAudioDuration(16000, time.Second), 16000) {
		t.Fatalf("expected stale partial to be ignored")
	}
}

func newTestLocalSTTSession(t *testing.T, transcriber whisperTranscriber) *localSTTSession {
	t.Helper()
	baseCtx, cancel := context.WithCancel(context.Background())
	s := &localSTTSession{
		whisper:    transcriber,
		events:     make(chan STTEvent, 32),
		sessionID:  "test-session",
		baseCtx:    baseCtx,
		baseCancel: cancel,
		workCh:     make(chan sttWork, 1),
		workerDone: make(chan struct{}),
		partialCfg: localSTTPartialConfig{
			Enabled:     true,
			MinInterval: 0,
			MinAudio:    200 * time.Millisecond,
			MaxTail:     4 * time.Second,
			Timeout:     2 * time.Second,
		},
	}
	go s.worker()
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func waitForSTTEventType(t *testing.T, ch <-chan STTEvent, typ STTEventType, timeout time.Duration) STTEvent {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for event type %q", typ)
		case evt, ok := <-ch:
			if !ok {
				t.Fatalf("event channel closed while waiting for %q", typ)
			}
			if evt.Type == typ {
				return evt
			}
		}
	}
}

func ensureNoSTTEventType(t *testing.T, ch <-chan STTEvent, typ STTEventType, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if evt.Type == typ {
				t.Fatalf("unexpected event type %q (text=%q)", typ, evt.Text)
			}
		}
	}
}

func zeroPCMBase64(size int) string {
	if size <= 0 {
		size = 2
	}
	return base64.StdEncoding.EncodeToString(make([]byte, size))
}
