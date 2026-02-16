package main

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ent0n29/samantha/internal/audio"
)

func TestDecodeWAVPCM16MonoRoundTrip(t *testing.T) {
	pcm := []byte{
		0x00, 0x00,
		0xE8, 0x03, // 1000
		0x18, 0xFC, // -1000
	}
	wav, err := audio.EncodeWAVPCM16LE(pcm, 16000)
	if err != nil {
		t.Fatalf("EncodeWAVPCM16LE() error = %v", err)
	}
	gotPCM, gotSR, err := decodeWAVPCM16(wav)
	if err != nil {
		t.Fatalf("decodeWAVPCM16() error = %v", err)
	}
	if gotSR != 16000 {
		t.Fatalf("sampleRate = %d, want 16000", gotSR)
	}
	if !bytes.Equal(gotPCM, pcm) {
		t.Fatalf("pcm mismatch: got=%v want=%v", gotPCM, pcm)
	}
}

func TestDecodeWAVPCM16StereoDownmix(t *testing.T) {
	// Frame 1: L=1000, R=-1000 => avg=0
	// Frame 2: L=3000, R=1000  => avg=2000
	stereo := []byte{
		0xE8, 0x03, 0x18, 0xFC,
		0xB8, 0x0B, 0xE8, 0x03,
	}
	wav := encodeWAV16Stereo(t, stereo, 24000)
	gotPCM, gotSR, err := decodeWAVPCM16(wav)
	if err != nil {
		t.Fatalf("decodeWAVPCM16() error = %v", err)
	}
	if gotSR != 24000 {
		t.Fatalf("sampleRate = %d, want 24000", gotSR)
	}
	if len(gotPCM) != 4 {
		t.Fatalf("len(gotPCM) = %d, want 4", len(gotPCM))
	}
	s1 := int16(binary.LittleEndian.Uint16(gotPCM[0:2]))
	s2 := int16(binary.LittleEndian.Uint16(gotPCM[2:4]))
	if s1 != 0 || s2 != 2000 {
		t.Fatalf("downmix samples = [%d %d], want [0 2000]", s1, s2)
	}
}

func encodeWAV16Stereo(t *testing.T, stereoPCM []byte, sampleRate int) []byte {
	t.Helper()
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if len(stereoPCM)%4 != 0 {
		t.Fatalf("stereoPCM length must be multiple of 4, got %d", len(stereoPCM))
	}
	dataSize := uint32(len(stereoPCM))
	byteRate := uint32(sampleRate * 2 * 16 / 8)
	blockAlign := uint16(2 * 16 / 8)

	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36)+dataSize)
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&b, binary.LittleEndian, uint16(2)) // stereo
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&b, binary.LittleEndian, byteRate)
	_ = binary.Write(&b, binary.LittleEndian, blockAlign)
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, dataSize)
	b.Write(stereoPCM)
	return b.Bytes()
}
