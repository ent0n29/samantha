package protocol

import (
	"errors"
	"testing"
)

func TestParseClientMessageAudioChunk(t *testing.T) {
	raw := []byte(`{"type":"client_audio_chunk","session_id":"s1","seq":1,"pcm16_base64":"AQID","sample_rate":16000,"ts_ms":123}`)
	msg, err := ParseClientMessage(raw)
	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}

	audio, ok := msg.(ClientAudioChunk)
	if !ok {
		t.Fatalf("message type = %T, want ClientAudioChunk", msg)
	}
	if audio.SessionID != "s1" || audio.SampleRate != 16000 {
		t.Fatalf("unexpected audio chunk: %+v", audio)
	}
}

func TestParseClientMessageRejectsUnknownType(t *testing.T) {
	_, err := ParseClientMessage([]byte(`{"type":"wat"}`))
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("error = %v, want ErrUnsupportedType", err)
	}
}

func TestParseClientMessageControl(t *testing.T) {
	raw := []byte(`{"type":"client_control","session_id":"s1","action":"stop","reason":"silence_release","ts_ms":456}`)
	msg, err := ParseClientMessage(raw)
	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}

	control, ok := msg.(ClientControl)
	if !ok {
		t.Fatalf("message type = %T, want ClientControl", msg)
	}
	if control.SessionID != "s1" || control.Action != "stop" {
		t.Fatalf("unexpected client control: %+v", control)
	}
	if control.TSMs != 456 {
		t.Fatalf("TSMs = %d, want %d", control.TSMs, 456)
	}
	if control.Reason != "silence_release" {
		t.Fatalf("Reason = %q, want %q", control.Reason, "silence_release")
	}
}

func TestParseClientMessageTaskControl(t *testing.T) {
	raw := []byte(`{"type":"client_control","session_id":"s1","action":"approve_task_step","task_id":"t1","approved":true}`)
	msg, err := ParseClientMessage(raw)
	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}

	control, ok := msg.(ClientControl)
	if !ok {
		t.Fatalf("message type = %T, want ClientControl", msg)
	}
	if control.TaskID != "t1" {
		t.Fatalf("TaskID = %q, want %q", control.TaskID, "t1")
	}
	if control.Approved == nil || !*control.Approved {
		t.Fatalf("Approved = %v, want true", control.Approved)
	}
}

func TestParseClientMessageRejectsInvalidAudioChunk(t *testing.T) {
	_, err := ParseClientMessage([]byte(`{"type":"client_audio_chunk","session_id":"","pcm16_base64":"","sample_rate":0}`))
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func BenchmarkParseClientMessageAudioChunk(b *testing.B) {
	raw := []byte(`{"type":"client_audio_chunk","session_id":"s1","seq":7,"pcm16_base64":"AQIDBAUGBwgJCgsMDQ4P","sample_rate":16000,"ts_ms":123456}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, err := ParseClientMessage(raw)
		if err != nil {
			b.Fatalf("ParseClientMessage() error = %v", err)
		}
		if _, ok := msg.(ClientAudioChunk); !ok {
			b.Fatalf("message type = %T, want ClientAudioChunk", msg)
		}
	}
}
