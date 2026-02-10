package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ent0n29/samantha/internal/audio"
)

type voiceSummary struct {
	VoiceID  string            `json:"voice_id"`
	Name     string            `json:"name"`
	Category string            `json:"category,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type listVoicesResponse struct {
	DefaultVoiceID string         `json:"default_voice_id"`
	Recommended    []voiceSummary `json:"recommended"`
	Voices         []voiceSummary `json:"voices"`
}

func (s *Server) handleListVoices(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(strings.TrimSpace(s.cfg.VoiceProvider), "local") {
		defaultID := strings.TrimSpace(s.cfg.LocalKokoroVoice)
		if defaultID == "" {
			defaultID = "af_heart"
		}

		voices := []voiceSummary{
			{VoiceID: "af_heart", Name: "Heart (Kokoro, US, warm)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "american"}},
			{VoiceID: "af_bella", Name: "Bella (Kokoro, US, bright)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "american"}},
			{VoiceID: "af_river", Name: "River (Kokoro, US, clear)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "american"}},
			{VoiceID: "af_sarah", Name: "Sarah (Kokoro, US, intimate)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "american"}},
			{VoiceID: "af_sky", Name: "Sky (Kokoro, US, light)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "american"}},
			{VoiceID: "af_nicole", Name: "Nicole (Kokoro, US, steady)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "american"}},
			{VoiceID: "bf_emma", Name: "Emma (Kokoro, UK, velvety)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "british"}},
			{VoiceID: "bf_isabella", Name: "Isabella (Kokoro, UK, crisp)", Category: "kokoro", Labels: map[string]string{"gender": "female", "accent": "british"}},
		}

		recommended := []voiceSummary{
			voices[0], // af_heart
			voices[1], // af_bella
			voices[6], // bf_emma
		}

		respondJSON(w, http.StatusOK, listVoicesResponse{
			DefaultVoiceID: defaultID,
			Recommended:    recommended,
			Voices:         voices,
		})
		return
	}

	if strings.TrimSpace(s.cfg.ElevenLabsAPIKey) == "" {
		respondJSON(w, http.StatusOK, listVoicesResponse{
			DefaultVoiceID: s.cfg.ElevenLabsTTSVoice,
			Recommended:    []voiceSummary{},
			Voices:         []voiceSummary{},
		})
		return
	}

	ctx := r.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.elevenlabs.io/v1/voices", nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	req.Header.Set("xi-api-key", s.cfg.ElevenLabsAPIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		respondError(w, http.StatusBadGateway, "elevenlabs_request_failed", err.Error())
		return
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respondError(w, http.StatusBadGateway, "elevenlabs_bad_status", fmt.Sprintf("status %d: %s", res.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	var parsed struct {
		Voices []struct {
			VoiceID  string            `json:"voice_id"`
			Name     string            `json:"name"`
			Category string            `json:"category"`
			Labels   map[string]string `json:"labels"`
		} `json:"voices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		respondError(w, http.StatusBadGateway, "elevenlabs_invalid_json", err.Error())
		return
	}

	all := make([]voiceSummary, 0, len(parsed.Voices))
	byID := make(map[string]voiceSummary, len(parsed.Voices))
	for _, v := range parsed.Voices {
		labels := v.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		if strings.ToLower(strings.TrimSpace(labels["gender"])) != "female" {
			continue
		}
		item := voiceSummary{
			VoiceID:  strings.TrimSpace(v.VoiceID),
			Name:     strings.TrimSpace(v.Name),
			Category: strings.TrimSpace(v.Category),
			Labels:   labels,
		}
		if item.VoiceID == "" || item.Name == "" {
			continue
		}
		all = append(all, item)
		byID[item.VoiceID] = item
	}

	sort.Slice(all, func(i, j int) bool {
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})

	recommendedIDs := []string{
		"cgSgspJ2msm6clMCkdW9", // Jessica
		"pFZP5JQG7iQjIQuC4Bku", // Lily
		"EXAVITQu4vr4xnSDxMaL", // Sarah
	}
	recommended := make([]voiceSummary, 0, len(recommendedIDs))
	for _, id := range recommendedIDs {
		if v, ok := byID[id]; ok {
			recommended = append(recommended, v)
		}
	}

	respondJSON(w, http.StatusOK, listVoicesResponse{
		DefaultVoiceID: s.cfg.ElevenLabsTTSVoice,
		Recommended:    recommended,
		Voices:         all,
	})
}

type previewTTSRequest struct {
	VoiceID   string `json:"voice_id"`
	PersonaID string `json:"persona_id"`
	Text      string `json:"text"`
}

func (s *Server) handlePreviewTTS(w http.ResponseWriter, r *http.Request) {
	if s.orchestrator == nil {
		respondError(w, http.StatusNotImplemented, "unavailable", "orchestrator not configured")
		return
	}
	// Only require an ElevenLabs key if we are actually using ElevenLabs.
	if strings.EqualFold(strings.TrimSpace(s.cfg.VoiceProvider), "elevenlabs") && strings.TrimSpace(s.cfg.ElevenLabsAPIKey) == "" {
		respondError(w, http.StatusBadRequest, "missing_elevenlabs_key", "ELEVENLABS_API_KEY is not set")
		return
	}

	var req previewTTSRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	voiceID := strings.TrimSpace(req.VoiceID)
	if voiceID == "" {
		voiceID = s.cfg.ElevenLabsTTSVoice
		if strings.EqualFold(strings.TrimSpace(s.cfg.VoiceProvider), "local") {
			voiceID = strings.TrimSpace(s.cfg.LocalKokoroVoice)
			if voiceID == "" {
				voiceID = "af_heart"
			}
		}
	}
	personaID := strings.TrimSpace(req.PersonaID)
	if personaID == "" {
		personaID = "warm"
	}
	text := strings.TrimSpace(req.Text)

	modelID := s.cfg.ElevenLabsTTSModel
	if strings.EqualFold(strings.TrimSpace(s.cfg.VoiceProvider), "local") {
		modelID = "kokoro"
	}

	audio, format, err := s.orchestrator.PreviewTTS(r.Context(), voiceID, modelID, personaID, text)
	if err != nil {
		respondError(w, http.StatusBadGateway, "tts_preview_failed", err.Error())
		return
	}

	contentType := mimeForTTSFormat(format)
	out := audio
	if sampleRate, ok := pcmSampleRate(format); ok {
		wav, err := audio2wav(out, sampleRate)
		if err != nil {
			respondError(w, http.StatusBadGateway, "tts_preview_failed", err.Error())
			return
		}
		out = wav
		contentType = "audio/wav"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	if strings.TrimSpace(format) != "" {
		w.Header().Set("X-Audio-Format", strings.TrimSpace(format))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func mimeForTTSFormat(format string) string {
	f := strings.ToLower(strings.TrimSpace(format))
	switch {
	case strings.Contains(f, "wav"):
		return "audio/wav"
	case strings.Contains(f, "mp3"):
		return "audio/mpeg"
	case strings.Contains(f, "ogg"):
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

func audio2wav(pcm []byte, sampleRate int) ([]byte, error) {
	return audio.EncodeWAVPCM16LE(pcm, sampleRate)
}

func pcmSampleRate(format string) (int, bool) {
	f := strings.ToLower(strings.TrimSpace(format))
	idx := strings.Index(f, "pcm_")
	if idx < 0 {
		return 0, false
	}
	rest := f[idx+len("pcm_"):]
	n := 0
	for n < len(rest) {
		c := rest[n]
		if c < '0' || c > '9' {
			break
		}
		n++
	}
	if n == 0 {
		return 16000, true
	}
	sr, err := strconv.Atoi(rest[:n])
	if err != nil || sr <= 0 {
		return 16000, true
	}
	return sr, true
}
