package httpserver

import "net/http"

// handleAccountUsage reports how much of the plan this account has used
// and how long its login lasts.
//
// It is a separate request from the account list on purpose: reading
// credentials and asking Anthropic takes a moment, and the list has to
// paint immediately whether or not this succeeds.
func (s *Server) handleAccountUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := s.manager.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// `?refresh=1` is what the Refresh button sends: without it a click
	// inside the cache window would return the same numbers and look
	// broken.
	if r.URL.Query().Get("refresh") != "" {
		s.usage.Forget(account.ID)
	}

	writeJSON(w, http.StatusOK, s.usage.Get(r.Context(), account.ID, account.ConfigDir))
}
