package httpserver

import (
	"net/http"

	"ccam/internal/buildinfo"
	"ccam/internal/service"
)

type statusResponse struct {
	// Service is a fixed marker so callers can tell a real ccam apart
	// from whatever else might be listening on a recorded port.
	Service string `json:"service"`
	Version string `json:"version"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{
		Service: service.StatusServiceName,
		Version: buildinfo.Version,
	})
}
