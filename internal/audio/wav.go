package audio

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
)

// EncodeWAVPCM16LE wraps raw PCM16LE mono audio bytes in a WAV container.
func EncodeWAVPCM16LE(pcm []byte, sampleRate int) ([]byte, error) {
	var buf bytes.Buffer
	if err := WriteWAVPCM16LETo(&buf, pcm, sampleRate); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteWAVPCM16LEFile writes raw PCM16LE mono audio bytes as a WAV file.
func WriteWAVPCM16LEFile(path string, pcm []byte, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteWAVPCM16LETo(f, pcm, sampleRate)
}

// WriteWAVPCM16LETo writes raw PCM16LE mono audio bytes to out as a WAV stream.
func WriteWAVPCM16LETo(out io.Writer, pcm []byte, sampleRate int) error {
	const (
		numChannels   = 1
		bitsPerSample = 16
		audioFormat   = 1 // PCM
	)
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * numChannels * bitsPerSample / 8)
	blockAlign := uint16(numChannels * bitsPerSample / 8)

	w := bufio.NewWriter(out)

	// RIFF header.
	if _, err := w.WriteString("RIFF"); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(36)+dataSize); err != nil {
		return err
	}
	if _, err := w.WriteString("WAVE"); err != nil {
		return err
	}

	// fmt chunk.
	if _, err := w.WriteString("fmt "); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(audioFormat)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(numChannels)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, blockAlign); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(bitsPerSample)); err != nil {
		return err
	}

	// data chunk.
	if _, err := w.WriteString("data"); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, dataSize); err != nil {
		return err
	}
	if _, err := w.Write(pcm); err != nil {
		return err
	}
	return w.Flush()
}
