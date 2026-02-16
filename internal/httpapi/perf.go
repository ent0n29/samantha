package httpapi

import "net/http"

func (s *Server) handlePerfLatency(w http.ResponseWriter, _ *http.Request) {
	if s.metrics == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"generated_at": "",
			"window_size":  0,
			"stages":       []any{},
			"indicators":   []any{},
		})
		return
	}
	respondJSON(w, http.StatusOK, s.metrics.SnapshotTurnStages())
}

func (s *Server) handlePerfLatencyReset(w http.ResponseWriter, _ *http.Request) {
	if s.metrics != nil {
		s.metrics.ResetTurnStages()
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	})
}
