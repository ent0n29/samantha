package httpapi

import "net/http"

func (s *Server) handlePerfLatency(w http.ResponseWriter, _ *http.Request) {
	if s.metrics == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"generated_at": "",
			"window_size":  0,
			"stages":       []any{},
		})
		return
	}
	respondJSON(w, http.StatusOK, s.metrics.SnapshotTurnStages())
}
