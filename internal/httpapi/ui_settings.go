package httpapi

import "net/http"

type uiSettingsResponse struct {
	UIAudioWorklet        bool   `json:"ui_audio_worklet"`
	TaskRuntimeEnabled    bool   `json:"task_runtime_enabled"`
	TaskDeskDefault       bool   `json:"task_desk_default"`
	SilenceBreakerMode    string `json:"silence_breaker_mode"`
	SilenceBreakerDelayMS int64  `json:"silence_breaker_delay_ms"`
	VADProfile            string `json:"vad_profile"`
	VADMinUtteranceMS     int64  `json:"vad_min_utterance_ms"`
	VADGraceMS            int64  `json:"vad_grace_ms"`
	AudioOverlapMS        int64  `json:"audio_overlap_ms"`
	LocalSTTProfile       string `json:"local_stt_profile"`
	FillerMode            string `json:"filler_mode"`
	FillerMinDelayMS      int64  `json:"filler_min_delay_ms"`
	FillerCooldownMS      int64  `json:"filler_cooldown_ms"`
	FillerMaxPerTurn      int    `json:"filler_max_per_turn"`
}

func (s *Server) handleUISettings(w http.ResponseWriter, _ *http.Request) {
	taskRuntimeEnabled := s.taskService != nil && s.taskService.Enabled()
	if s.taskService == nil {
		taskRuntimeEnabled = s.cfg.TaskRuntimeEnabled
	}
	respondJSON(w, http.StatusOK, uiSettingsResponse{
		UIAudioWorklet:        s.cfg.UIAudioWorklet,
		TaskRuntimeEnabled:    taskRuntimeEnabled,
		TaskDeskDefault:       s.cfg.UITaskDeskDefault,
		SilenceBreakerMode:    s.cfg.UISilenceBreakerMode,
		SilenceBreakerDelayMS: s.cfg.UISilenceBreakerDelay.Milliseconds(),
		VADProfile:            s.cfg.UIVADProfile,
		VADMinUtteranceMS:     s.cfg.UIVADMinUtterance.Milliseconds(),
		VADGraceMS:            s.cfg.UIVADGrace.Milliseconds(),
		AudioOverlapMS:        s.cfg.UIAudioSegmentOverlap.Milliseconds(),
		LocalSTTProfile:       s.cfg.LocalSTTProfile,
		FillerMode:            s.cfg.UIFillerMode,
		FillerMinDelayMS:      s.cfg.UIFillerMinDelay.Milliseconds(),
		FillerCooldownMS:      s.cfg.UIFillerCooldown.Milliseconds(),
		FillerMaxPerTurn:      s.cfg.UIFillerMaxPerTurn,
	})
}
