// Package handler is the ccam panel as a Vercel serverless function.
//
// It is the same panel `ccam panel serve` runs, wired to the two things a
// serverless host provides instead of a local disk: a Postgres database for the
// state, and an environment variable for the key that seals the logins. Every
// request may be a fresh instance, which is exactly why the state lives in
// Postgres (with the one-holder rule held by a compare-and-swap) and why the
// admin session is a sealed cookie rather than anything kept in memory.
//
// Vercel builds each file under api/ into its own function and calls the
// exported Handler. One vercel.json rewrite points every path here, so the
// whole panel — its API and its embedded UI — is served by this one function.
package handler

import (
	"context"
	"net/http"
	"os"
	"sync"

	"ccam/internal/panel"
	"ccam/internal/panelpg"
)

var (
	once     sync.Once
	built    http.Handler
	buildErr error
)

// build wires the panel once and reuses it across warm invocations. A cold
// instance pays the connect cost; a warm one does not.
func build() (http.Handler, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errMissing("DATABASE_URL", "a Postgres connection string")
	}
	keyEnc := os.Getenv("CCAM_PANEL_KEY")
	if keyEnc == "" {
		return nil, errMissing("CCAM_PANEL_KEY", "the base64 key that seals stored logins")
	}

	secret, err := panel.SecretFromBase64(keyEnc)
	if err != nil {
		return nil, err
	}
	back, err := panelpg.Open(context.Background(), dsn)
	if err != nil {
		return nil, err
	}
	store := panel.NewStoreWithBackend(back)
	return panel.NewServer(store, secret).Handler(), nil
}

// Handler is Vercel's entry point.
func Handler(w http.ResponseWriter, r *http.Request) {
	once.Do(func() { built, buildErr = build() })
	if buildErr != nil {
		http.Error(w, "The panel is not configured: "+buildErr.Error(), http.StatusInternalServerError)
		return
	}
	built.ServeHTTP(w, r)
}

type missingEnv struct{ name, what string }

func (e missingEnv) Error() string {
	return "set " + e.name + " (" + e.what + ") in the deployment's environment"
}
func errMissing(name, what string) error { return missingEnv{name, what} }
