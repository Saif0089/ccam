package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/ptyauth"
)

// loginRetentionAfterFinish is how long a finished login's events stay
// available for a late or reloading browser to pick up.
const loginRetentionAfterFinish = 30 * time.Second

// handleStartLogin begins a browser-driven login for one account. It
// returns as soon as the attempt is under way; progress is delivered
// via GET .../login/events.
//
// An attempt already in progress is reused rather than restarted: the
// user may simply have reloaded the page, and killing the running
// `claude` would invalidate the OAuth URL they already have open.
func (s *Server) handleStartLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, err := s.manager.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Held across the whole start: the Connect button can be clicked
	// twice, and a check-then-act here would let both requests see "no
	// login in progress" and spawn a `claude` each, with the loser left
	// running unreferenced for its full timeout.
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	existing := s.logins[id]
	s.mu.Unlock()
	if existing != nil && !existing.finished() {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	s.cancelLogin(id) // retire a finished attempt before starting a new one

	session, err := ptyauth.Start(context.Background(), ptyauth.Config{
		ClaudeBinary: s.claudeBinary,
		ConfigDir:    account.ConfigDir,
		Env:          accounts.EnvForConfigDir(account.ConfigDir),
		Timeout:      10 * time.Minute,
		PollInterval: 2 * time.Second,
		Prober:       &accounts.Prober{ClaudeBinary: s.claudeBinary},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	broadcast := newLoginBroadcast(session)
	s.mu.Lock()
	s.logins[id] = broadcast
	s.mu.Unlock()

	go broadcast.run(session.Events())
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
			// Status first, and only then the slower work: the browser
			// refreshes its account list as soon as it sees this event,
			// so anything ahead of SetStatus is time the UI spends
			// saying "Connected" beside a row still marked pending.
			account, err := s.manager.SetStatus(id, accounts.StatusLinked)
			if err != nil {
				log.Printf("login succeeded for %s but recording it failed: %v", id, err)
				continue
			}
			_ = s.syncAliases()

			// Claude Code treats logging in and finishing onboarding as
			// separate things, so without this the first `claude` run
			// under a freshly linked account walks the whole first-run
			// wizard and asks to pick a login method on an account that
			// is already signed in.
			if err := accounts.MarkOnboarded(account.ConfigDir, s.claudeBinary); err != nil {
				log.Printf("could not mark onboarding complete for %s: %v", id, err)
			}
		}
	}

	time.AfterFunc(loginRetentionAfterFinish, func() {
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
	// Get the headers onto the wire now rather than at the first event,
	// so the browser's EventSource actually opens while `claude` is
	// still starting up.
	flusher.Flush()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				// The login is over. Browsers reconnect to a closed SSE
				// stream after ~3s by default, which would replay this
				// whole history on a loop; a long retry interval tells
				// them not to bother.
				fmt.Fprint(w, "retry: 86400000\n\n")
				flusher.Flush()
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

type submitCodeRequest struct {
	Code string `json:"code"`
}

// handleSubmitLoginCode types an authorization code into the waiting
// `claude`, for the flow where the browser hands the user a code to
// paste back rather than redirecting to a local callback.
func (s *Server) handleSubmitLoginCode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req submitCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	s.mu.Lock()
	broadcast := s.logins[id]
	s.mu.Unlock()
	if broadcast == nil {
		writeError(w, http.StatusNotFound, "no login in progress for this account")
		return
	}

	if err := broadcast.submitCode(req.Code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		broadcast.stop()
	}
}

// stopAllLogins ends every in-flight login, so shutting the server down
// doesn't leave orphaned `claude` processes behind.
func (s *Server) stopAllLogins() {
	s.mu.Lock()
	pending := make([]*loginBroadcast, 0, len(s.logins))
	for id, b := range s.logins {
		pending = append(pending, b)
		delete(s.logins, id)
	}
	s.mu.Unlock()
	for _, b := range pending {
		b.stop()
	}
}
