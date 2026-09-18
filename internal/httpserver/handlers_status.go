package httpserver

import (
	"net/http"
	"os"
	"runtime"

	"clawdh/internal/buildinfo"
	"clawdh/internal/service"
)

type statusResponse struct {
	// Service is a fixed marker so callers can tell a real clawdh apart
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
	// OS is this machine's operating system (runtime.GOOS). The page shows the
	// command to run each account, and which one is right depends on the machine
	// clawdh runs on — where the shell aliases live — not on the browser's OS,
	// which may be a different computer viewing the page. Windows gets no
	// `claude-<name>` alias, so there the page shows `clawdh <name>` instead.
	OS string `json:"os"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{
		Service: service.StatusServiceName,
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
		Tag:     buildinfo.Tag(),
		PID:     os.Getpid(),
		OS:      runtime.GOOS,
	})
}
