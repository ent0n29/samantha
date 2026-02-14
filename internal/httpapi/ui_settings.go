package httpapi

import "net/http"

type uiSettingsResponse struct {
	UIAudioWorklet        bool   `json:"ui_audio_worklet"`
	TaskRuntimeEnabled    bool   `json:"task_runtime_enabled"`
	TaskDeskDefault       bool   `json:"task_desk_default"`
	SilenceBreakerMode    string `json:"silence_breaker_mode"`
	SilenceBreakerDelayMS int64  `json:"silence_breaker_delay_ms"`
	VADProfile            string `json:"vad_profile"`
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
	})
}
