package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/ptyauth"
)

// handleStartLogin begins (or restarts) a browser-driven login for one
// account. It returns as soon as the attempt is under way; progress is
// delivered via GET .../login/events.
func (s *Server) handleStartLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := s.manager.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	s.cancelLogin(id) // superseding a stuck/abandoned attempt is fine

	ctx, cancel := context.WithCancel(context.Background())
	broadcast := newLoginBroadcast(cancel)

	s.mu.Lock()
	s.logins[id] = broadcast
	s.mu.Unlock()

	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+account.ConfigDir)
	events := ptyauth.Run(ctx, ptyauth.Config{
		ClaudeBinary: s.claudeBinary,
		ConfigDir:    account.ConfigDir,
		Env:          env,
		Timeout:      5 * time.Minute,
	})

	go broadcast.run(events)
	go s.watchLoginOutcome(id, broadcast)

	w.WriteHeader(http.StatusAccepted)
}

// watchLoginOutcome updates the account's stored status once its login
// attempt reaches a terminal state, and retires the broadcast entry
// once no one could still be subscribing to it.
func (s *Server) watchLoginOutcome(id string, broadcast *loginBroadcast) {
	ch, unsubscribe := broadcast.subscribe()
	defer unsubscribe()
	for ev := range ch {
		if ev.Type == ptyauth.EventLinked {
			if _, err := s.manager.SetStatus(id, accounts.StatusLinked); err == nil {
				_ = s.syncAliases()
			}
		}
	}

	time.AfterFunc(time.Minute, func() {
		s.mu.Lock()
		if s.logins[id] == broadcast {
			delete(s.logins, id)
		}
		s.mu.Unlock()
	})
}

// handleLoginEvents streams one account's login events as SSE.
func (s *Server) handleLoginEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.Lock()
	broadcast := s.logins[id]
	s.mu.Unlock()
	if broadcast == nil {
		writeError(w, http.StatusNotFound, "no login in progress for this account; POST to login first")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch, unsubscribe := broadcast.subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleCancelLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.cancelLogin(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) cancelLogin(id string) {
	s.mu.Lock()
	broadcast := s.logins[id]
	delete(s.logins, id)
	s.mu.Unlock()
	if broadcast != nil {
		broadcast.cancel()
	}
}
