package httpserver

import "net/http"

// handleAccountUsage reports how much of the plan this account has used
// and how long its login lasts.
//
// It is a separate request from the account list on purpose: reading
// credentials and asking Anthropic takes a moment, and the list has to
// paint immediately whether or not this succeeds.
//
// It never asks Anthropic sooner than the cache allows. A GET passes
// withLocalOnly from any website open in the same browser, so the one way
// to ask for newer numbers is the POST below, which that check covers.
func (s *Server) handleAccountUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := s.manager.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.usage.Get(r.Context(), account.ID, account.ConfigDir))
}

// handleExpireUsage marks an account's cached numbers out of date, for
// something that knows they have just moved — switching accounts does —
// but has no use for the numbers itself. Nothing is read here: the next
// request for this account's usage does that, under the same limits as
// any other.
func (s *Server) handleExpireUsage(w http.ResponseWriter, r *http.Request) {
	account, err := s.manager.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.usage.Expire(account.ID)
	w.WriteHeader(http.StatusNoContent)
}
