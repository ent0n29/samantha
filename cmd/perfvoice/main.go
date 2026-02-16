package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ent0n29/samantha/internal/protocol"
)

type options struct {
	baseURL        string
	userID         string
	personaID      string
	voiceID        string
	modelID        string
	turns          int
	chunkMS        int
	realtime       float64
	startDelay     time.Duration
	interTurnDelay time.Duration
	turnTimeout    time.Duration
	texts          []string
	verbose        bool
}

type createSessionRequest struct {
	UserID    string `json:"user_id,omitempty"`
	PersonaID string `json:"persona_id,omitempty"`
	VoiceID   string `json:"voice_id,omitempty"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
}

type previewRequest struct {
	VoiceID   string `json:"voice_id,omitempty"`
	PersonaID string `json:"persona_id,omitempty"`
	ModelID   string `json:"model_id,omitempty"`
	Text      string `json:"text,omitempty"`
}

type wsEnvelope struct {
	Type      string `json:"type"`
	TurnID    string `json:"turn_id,omitempty"`
	Code      string `json:"code,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Text      string `json:"text,omitempty"`
	TextDelta string `json:"text_delta,omitempty"`
}

type audioClip struct {
	Text       string
	PCM16LE    []byte
	SampleRate int
}

var defaultUtterances = []string{
	"Reply in three words: latency bottleneck?",
	"Reply in three words: next optimization?",
	"Reply in three words: architecture summary?",
	"Reply in three words: top risk?",
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "perfvoice: %v\n", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "perfvoice: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() (options, error) {
	var cfg options
	var textsRaw string
	var startDelayMS int
	var interTurnMS int
	var turnTimeoutMS int

	flag.StringVar(&cfg.baseURL, "base-url", "http://127.0.0.1:8080", "Samantha base URL")
	flag.StringVar(&cfg.userID, "user-id", "perf-replay", "user_id used for the synthetic session")
	flag.StringVar(&cfg.personaID, "persona-id", "concise", "persona_id used for synthetic turns")
	flag.StringVar(&cfg.voiceID, "voice-id", "", "optional voice_id for preview synthesis")
	flag.StringVar(&cfg.modelID, "model-id", "", "optional model_id for preview synthesis")
	flag.IntVar(&cfg.turns, "turns", 10, "number of turns to replay")
	flag.IntVar(&cfg.chunkMS, "chunk-ms", 45, "audio chunk size in milliseconds")
	flag.Float64Var(&cfg.realtime, "realtime", 3.0, "chunk pacing multiplier (1.0=realtime, 2.0=2x)")
	flag.IntVar(&startDelayMS, "start-delay-ms", 900, "delay before first synthetic turn in milliseconds")
	flag.IntVar(&interTurnMS, "inter-turn-ms", 180, "delay between turns in milliseconds")
	flag.IntVar(&turnTimeoutMS, "turn-timeout-ms", 15000, "timeout waiting for assistant_turn_end per turn in milliseconds")
	flag.StringVar(&textsRaw, "texts", "", "utterances separated by '|' (optional)")
	flag.BoolVar(&cfg.verbose, "verbose", true, "print replay progress")
	flag.Parse()

	cfg.baseURL = strings.TrimRight(strings.TrimSpace(cfg.baseURL), "/")
	if cfg.baseURL == "" {
		return options{}, fmt.Errorf("base-url is required")
	}
	if cfg.turns <= 0 {
		return options{}, fmt.Errorf("turns must be > 0")
	}
	if cfg.chunkMS < 10 || cfg.chunkMS > 2000 {
		return options{}, fmt.Errorf("chunk-ms must be in [10,2000]")
	}
	if cfg.realtime <= 0 {
		return options{}, fmt.Errorf("realtime must be > 0")
	}
	if startDelayMS < 0 {
		startDelayMS = 0
	}
	if interTurnMS < 0 {
		interTurnMS = 0
	}
	if turnTimeoutMS < 1000 {
		turnTimeoutMS = 1000
	}
	cfg.startDelay = time.Duration(startDelayMS) * time.Millisecond
	cfg.interTurnDelay = time.Duration(interTurnMS) * time.Millisecond
	cfg.turnTimeout = time.Duration(turnTimeoutMS) * time.Millisecond

	if strings.TrimSpace(textsRaw) == "" {
		cfg.texts = append([]string(nil), defaultUtterances...)
	} else {
		parts := strings.Split(textsRaw, "|")
		for _, part := range parts {
			t := strings.TrimSpace(part)
			if t != "" {
				cfg.texts = append(cfg.texts, t)
			}
		}
		if len(cfg.texts) == 0 {
			return options{}, fmt.Errorf("texts produced no non-empty utterances")
		}
	}
	return cfg, nil
}

func run(cfg options) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	httpClient := &http.Client{Timeout: 45 * time.Second}
	sessionID, err := createSession(ctx, httpClient, cfg)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer func() {
		_ = endSession(context.Background(), httpClient, cfg.baseURL, sessionID)
	}()

	if cfg.verbose {
		fmt.Printf("perfvoice: session=%s turns=%d chunk_ms=%d realtime=%.2f\n", sessionID, cfg.turns, cfg.chunkMS, cfg.realtime)
	}

	clips, err := synthClips(ctx, httpClient, cfg)
	if err != nil {
		return fmt.Errorf("prepare utterance audio: %w", err)
	}

	wsURL, err := wsURLForSession(cfg.baseURL, sessionID)
	if err != nil {
		return fmt.Errorf("build ws URL: %w", err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("open websocket: %w", err)
	}
	defer conn.Close()

	if cfg.startDelay > 0 {
		time.Sleep(cfg.startDelay)
	}

	turnEndCh := make(chan struct{}, 32)
	readErrCh := make(chan error, 1)
	go readLoop(conn, turnEndCh, readErrCh, cfg.verbose)

	seq := 0
	for i := 0; i < cfg.turns; i++ {
		select {
		case err := <-readErrCh:
			return fmt.Errorf("ws read: %w", err)
		default:
		}

		clip := clips[i%len(clips)]
		if cfg.verbose {
			fmt.Printf("perfvoice: turn %d/%d text=%q sample_rate=%dHz bytes=%d\n", i+1, cfg.turns, clip.Text, clip.SampleRate, len(clip.PCM16LE))
		}

		if err := sendTurnAudio(conn, sessionID, clip, cfg.chunkMS, cfg.realtime, &seq); err != nil {
			return fmt.Errorf("turn %d send audio: %w", i+1, err)
		}
		if err := sendStop(conn, sessionID); err != nil {
			return fmt.Errorf("turn %d send stop: %w", i+1, err)
		}
		if err := awaitTurnEnd(turnEndCh, readErrCh, cfg.turnTimeout); err != nil {
			return fmt.Errorf("turn %d await assistant_turn_end: %w", i+1, err)
		}
		if cfg.interTurnDelay > 0 && i < cfg.turns-1 {
			time.Sleep(cfg.interTurnDelay)
		}
	}

	if cfg.verbose {
		fmt.Println("perfvoice: replay completed")
	}
	return nil
}

func createSession(ctx context.Context, client *http.Client, cfg options) (string, error) {
	reqBody := createSessionRequest{
		UserID:    cfg.userID,
		PersonaID: cfg.personaID,
	}
	if strings.TrimSpace(cfg.voiceID) != "" {
		reqBody.VoiceID = strings.TrimSpace(cfg.voiceID)
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/v1/voice/session", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var out createSessionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.SessionID) == "" {
		return "", fmt.Errorf("missing session_id in response")
	}
	return out.SessionID, nil
}

func endSession(ctx context.Context, client *http.Client, baseURL, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/voice/session/"+url.PathEscape(sessionID)+"/end", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return nil
}

func synthClips(ctx context.Context, client *http.Client, cfg options) ([]audioClip, error) {
	cache := make(map[string]audioClip, len(cfg.texts))
	out := make([]audioClip, 0, len(cfg.texts))
	for _, text := range cfg.texts {
		if existing, ok := cache[text]; ok {
			out = append(out, existing)
			continue
		}
		clip, err := synthClip(ctx, client, cfg, text)
		if err != nil {
			return nil, err
		}
		cache[text] = clip
		out = append(out, clip)
	}
	return out, nil
}

func synthClip(ctx context.Context, client *http.Client, cfg options, text string) (audioClip, error) {
	reqBody := previewRequest{
		PersonaID: cfg.personaID,
		Text:      text,
	}
	if strings.TrimSpace(cfg.voiceID) != "" {
		reqBody.VoiceID = strings.TrimSpace(cfg.voiceID)
	}
	if strings.TrimSpace(cfg.modelID) != "" {
		reqBody.ModelID = strings.TrimSpace(cfg.modelID)
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return audioClip{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/v1/voice/tts/preview", bytes.NewReader(payload))
	if err != nil {
		return audioClip{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return audioClip{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 40<<20))
	if err != nil {
		return audioClip{}, err
	}
	if res.StatusCode != http.StatusOK {
		return audioClip{}, fmt.Errorf("preview %q HTTP %d: %s", text, res.StatusCode, strings.TrimSpace(string(body)))
	}

	pcm, sampleRate, err := decodeWAVPCM16(body)
	if err != nil {
		return audioClip{}, fmt.Errorf("decode preview wav for %q: %w", text, err)
	}
	if len(pcm) == 0 {
		return audioClip{}, fmt.Errorf("preview wav for %q produced no PCM bytes", text)
	}
	return audioClip{
		Text:       text,
		PCM16LE:    pcm,
		SampleRate: sampleRate,
	}, nil
}

func wsURLForSession(baseURL, sessionID string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported base-url scheme %q", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("base-url host is required")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/voice/session/ws"
	q := u.Query()
	q.Set("session_id", sessionID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func readLoop(conn *websocket.Conn, turnEndCh chan<- struct{}, readErrCh chan<- error, verbose bool) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case readErrCh <- err:
			default:
			}
			return
		}

		var env wsEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case string(protocol.TypeAssistantTurnEnd):
			select {
			case turnEndCh <- struct{}{}:
			default:
			}
		case string(protocol.TypeErrorEvent):
			if verbose {
				fmt.Fprintf(os.Stderr, "perfvoice: error_event code=%s detail=%s\n", env.Code, env.Detail)
			}
		}
	}
}

func sendTurnAudio(conn *websocket.Conn, sessionID string, clip audioClip, chunkMS int, realtime float64, seq *int) error {
	sampleRate := clip.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	bytesPerChunk := sampleRate * 2 * chunkMS / 1000
	if bytesPerChunk < 2 {
		bytesPerChunk = 2
	}
	if bytesPerChunk%2 != 0 {
		bytesPerChunk++
	}
	if bytesPerChunk > len(clip.PCM16LE) {
		bytesPerChunk = len(clip.PCM16LE)
		if bytesPerChunk%2 != 0 {
			bytesPerChunk--
		}
	}
	if bytesPerChunk <= 0 {
		return fmt.Errorf("invalid chunk size for sample_rate=%d", sampleRate)
	}

	for off := 0; off < len(clip.PCM16LE); {
		end := off + bytesPerChunk
		if end > len(clip.PCM16LE) {
			end = len(clip.PCM16LE)
		}
		if (end-off)%2 != 0 {
			end--
		}
		if end <= off {
			break
		}
		chunkBytes := end - off
		*seq = *seq + 1
		msg := protocol.ClientAudioChunk{
			Type:        protocol.TypeClientAudioChunk,
			SessionID:   sessionID,
			Seq:         *seq,
			PCM16Base64: base64.StdEncoding.EncodeToString(clip.PCM16LE[off:end]),
			SampleRate:  sampleRate,
			TSMs:        time.Now().UnixMilli(),
		}
		if err := conn.WriteJSON(msg); err != nil {
			return err
		}
		off = end

		chunkDuration := time.Duration(float64(time.Duration(chunkBytes)*time.Second/time.Duration(sampleRate*2)) / realtime)
		if chunkDuration <= 0 {
			chunkDuration = 10 * time.Millisecond
		}
		time.Sleep(chunkDuration)
	}
	return nil
}

func sendStop(conn *websocket.Conn, sessionID string) error {
	msg := protocol.ClientControl{
		Type:      protocol.TypeClientControl,
		SessionID: sessionID,
		Action:    "stop",
		Reason:    "perf_replay_turn_end",
		TSMs:      time.Now().UnixMilli(),
	}
	return conn.WriteJSON(msg)
}

func awaitTurnEnd(turnEndCh <-chan struct{}, readErrCh <-chan error, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-turnEndCh:
		return nil
	case err := <-readErrCh:
		return err
	case <-timer.C:
		return fmt.Errorf("timeout after %s", timeout)
	}
}

func decodeWAVPCM16(data []byte) ([]byte, int, error) {
	if len(data) < 12 {
		return nil, 0, fmt.Errorf("wav too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("unsupported wav header")
	}

	var (
		haveFmt     bool
		audioFormat uint16
		channels    uint16
		sampleRate  int
		bitsPerSamp uint16
		pcmData     []byte
	)
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		off += 8
		if size < 0 || off+size > len(data) {
			return nil, 0, fmt.Errorf("invalid wav chunk size")
		}
		chunk := data[off : off+size]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, fmt.Errorf("invalid wav fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bitsPerSamp = binary.LittleEndian.Uint16(chunk[14:16])
			haveFmt = true
		case "data":
			pcmData = append(pcmData[:0], chunk...)
		}
		off += size
		if size%2 == 1 {
			off++
		}
	}
	if !haveFmt {
		return nil, 0, fmt.Errorf("wav fmt chunk missing")
	}
	if len(pcmData) == 0 {
		return nil, 0, fmt.Errorf("wav data chunk missing")
	}
	if audioFormat != 1 {
		return nil, 0, fmt.Errorf("unsupported wav audio format %d", audioFormat)
	}
	if bitsPerSamp != 16 {
		return nil, 0, fmt.Errorf("unsupported wav bits_per_sample %d", bitsPerSamp)
	}
	if channels == 0 {
		return nil, 0, fmt.Errorf("invalid wav channels=0")
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	if channels == 1 {
		if len(pcmData)%2 != 0 {
			pcmData = pcmData[:len(pcmData)-1]
		}
		return pcmData, sampleRate, nil
	}

	frameBytes := int(channels) * 2
	if frameBytes <= 0 || len(pcmData) < frameBytes {
		return nil, 0, fmt.Errorf("invalid wav frame bytes")
	}
	frameCount := len(pcmData) / frameBytes
	mono := make([]byte, frameCount*2)
	for i := 0; i < frameCount; i++ {
		base := i * frameBytes
		sum := 0
		for ch := 0; ch < int(channels); ch++ {
			s := int16(binary.LittleEndian.Uint16(pcmData[base+ch*2 : base+ch*2+2]))
			sum += int(s)
		}
		avg := int16(sum / int(channels))
		binary.LittleEndian.PutUint16(mono[i*2:i*2+2], uint16(avg))
	}
	return mono, sampleRate, nil
}
