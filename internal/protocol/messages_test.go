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

func TestParseClientMessageRejectsInvalidAudioChunk(t *testing.T) {
	_, err := ParseClientMessage([]byte(`{"type":"client_audio_chunk","session_id":"","pcm16_base64":"","sample_rate":0}`))
	if err == nil {
		t.Fatalf("expected validation error")
	}
}
