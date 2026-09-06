package httpserver

import "net/http"

func (s *Server) handleLaunchTerminal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := s.manager.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := s.launchTerminal(account.ConfigDir, account.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
