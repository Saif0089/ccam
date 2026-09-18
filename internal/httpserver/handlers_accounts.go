package httpserver

import (
	"encoding/json"
	"net/http"

	"ccam/internal/accounts"
)

type accountsResponse struct {
	Accounts []accounts.Account `json:"accounts"`
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.manager.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountsResponse{Accounts: list})
}

type createAccountRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	account, err := s.manager.Add(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.syncAliases(); err != nil {
		writeError(w, http.StatusInternalServerError, "account created but syncing shell aliases failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

type renameAccountRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req renameAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	account, err := s.manager.Rename(id, req.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.syncAliases(); err != nil {
		writeError(w, http.StatusInternalServerError, "account renamed but syncing shell aliases failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.cancelLogin(id) // an in-progress login for this account can't outlive it
	s.usage.Forget(id)

	if _, err := s.manager.Remove(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.syncAliases(); err != nil {
		writeError(w, http.StatusInternalServerError, "account removed but syncing shell aliases failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// syncAliases rewrites every rc file's managed block. Accounts no longer get
// shell aliases — every account runs as `ccam <name>` or `ccam shared <name>`,
// one command shape on every OS — so the block carries only the `claude`
// wrapper that makes a plain `claude` session switchable. Syncing with no
// entries is also what removes aliases written by earlier versions.
func (s *Server) syncAliases() error {
	return s.syncer.Sync(nil)
}
