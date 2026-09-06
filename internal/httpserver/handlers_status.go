package httpserver

import (
	"net/http"

	"ccam/internal/buildinfo"
)

type statusResponse struct {
	Version string `json:"version"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Version: buildinfo.Version})
}
