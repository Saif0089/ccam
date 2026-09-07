package httpserver

import (
	"context"
	"log"
	"time"

	"ccam/internal/accounts"
)

// defaultCheckInterval throttles how often the default account is
// re-detected. Each check costs a `claude auth status` subprocess, so
// this is not something to do on every page load — but it does need to
// happen more than once, since the user may sign in to their default
// account after ccam has already started.
const defaultCheckInterval = 60 * time.Second

// ensureDefaultAccount adopts the account plain `claude` is signed in
// as, at most once per defaultCheckInterval.
func (s *Server) ensureDefaultAccount(ctx context.Context) {
	s.defaultCheckMu.Lock()
	if time.Since(s.defaultCheckedAt) < defaultCheckInterval {
		s.defaultCheckMu.Unlock()
		return
	}
	s.defaultCheckedAt = time.Now()
	s.defaultCheckMu.Unlock()

	prober := &accounts.Prober{ClaudeBinary: s.claudeBinary}
	added, err := s.manager.EnsureDefault(ctx, prober)
	if err != nil {
		log.Printf("could not check for the default claude account: %v", err)
		return
	}
	if added {
		log.Printf("adopted the account plain `claude` is signed in as")
	}
}

// StartDefaultAccountWatch adopts the default account now, and keeps
// checking in the background so it appears once the user signs in.
func (s *Server) StartDefaultAccountWatch(ctx context.Context) {
	go func() {
		s.ensureDefaultAccount(ctx)
		ticker := time.NewTicker(defaultCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.ensureDefaultAccount(ctx)
			}
		}
	}()
}
