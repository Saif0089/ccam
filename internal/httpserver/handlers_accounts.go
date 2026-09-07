package httpserver

import (
	"encoding/json"
	"net/http"

	"ccam/internal/accounts"
	"ccam/internal/shellrc"
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

// syncAliases rewrites every rc file's managed block to match the
// current account list. Called after every mutation so a fresh
// terminal always has an up-to-date alias.
func (s *Server) syncAliases() error {
	list, err := s.manager.List()
	if err != nil {
		return err
	}
	entries := make([]shellrc.AliasEntry, 0, len(list))
	for _, a := range list {
		// The default account is reached by typing `claude`; writing an
		// `alias claude=...` would be redundant and a good way to break
		// the user's actual claude command.
		if a.IsDefault() {
			continue
		}
		entries = append(entries, shellrc.AliasEntry{Alias: a.Alias, ConfigDir: a.ConfigDir})
	}
	return s.syncer.Sync(entries)
}
