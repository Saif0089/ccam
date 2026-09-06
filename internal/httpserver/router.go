package httpserver

import (
	"encoding/json"
	"net/http"

	"ccam/internal/httpserver/webui"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status", s.handleStatus)

	mux.HandleFunc("GET /api/accounts", s.handleListAccounts)
	mux.HandleFunc("POST /api/accounts", s.handleCreateAccount)
	mux.HandleFunc("PATCH /api/accounts/{id}", s.handleRenameAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.handleDeleteAccount)

	mux.HandleFunc("POST /api/accounts/{id}/login", s.handleStartLogin)
	mux.HandleFunc("GET /api/accounts/{id}/login/events", s.handleLoginEvents)
	mux.HandleFunc("POST /api/accounts/{id}/login/cancel", s.handleCancelLogin)

	mux.HandleFunc("POST /api/accounts/{id}/launch-terminal", s.handleLaunchTerminal)

	mux.Handle("/", webui.Handler())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
