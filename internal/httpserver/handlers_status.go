package httpserver

import (
	"net/http"
	"os"

	"ccam/internal/buildinfo"
	"ccam/internal/service"
)

type statusResponse struct {
	// Service is a fixed marker so callers can tell a real ccam apart
	// from whatever else might be listening on a recorded port.
	Service string `json:"service"`
	Version string `json:"version"`
	// Commit and Tag say which build is answering. The page shows Tag
	// in its corner, so "is the fix I just shipped actually running?"
	// is a question you answer by looking rather than by guessing.
	Commit string `json:"commit,omitempty"`
	Tag    string `json:"tag"`
	// PID lets a caller confirm this is *its* server — the one its
	// pidfile names — rather than another installation's.
	PID int `json:"pid"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{
		Service: service.StatusServiceName,
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
		Tag:     buildinfo.Tag(),
		PID:     os.Getpid(),
	})
}
