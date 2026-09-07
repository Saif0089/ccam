package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCredsFile stores a login whose clocks are relative to now: fixed
// timestamps in a fixture quietly become "expired" as the months pass,
// and the test then fails for a reason that has nothing to do with it.
func writeCredsFile(t *testing.T, dir string, access, refresh time.Time) {
	t.Helper()
	writeFile(t, filepath.Join(dir, ".credentials.json"), fmt.Sprintf(
		`{"claudeAiOauth":{"accessToken":"tok","subscriptionType":"max","expiresAt":%d,"refreshTokenExpiresAt":%d}}`,
		access.UnixMilli(), refresh.UnixMilli()))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestServiceCachesAndForgets(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, time.Now().Add(8*time.Hour), time.Now().Add(30*24*time.Hour))

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(realPayload))
	}))
	defer srv.Close()

	svc := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()})

	first := svc.Get(context.Background(), "work", dir)
	if first.Usage == nil {
		t.Fatalf("no usage: %s", first.Error)
	}
	if first.Session == nil || first.Session.Plan != "max" {
		t.Fatalf("session = %+v", first.Session)
	}
	if first.Session.AccessExpiresAt == nil || first.Session.SessionExpiresAt == nil {
		t.Fatal("both clocks should be reported")
	}

	svc.Get(context.Background(), "work", dir)
	if calls != 1 {
		t.Errorf("made %d calls, want the second one served from cache", calls)
	}

	svc.Forget("work")
	svc.Get(context.Background(), "work", dir)
	if calls != 2 {
		t.Errorf("made %d calls, want a refetch after Forget", calls)
	}
}

func TestServiceExpiresCache(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"tok"}}`)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(realPayload))
	}))
	defer srv.Close()

	svc := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()})
	now := time.Now()
	svc.now = func() time.Time { return now }

	svc.Get(context.Background(), "work", dir)
	now = now.Add(TTL + time.Second)
	svc.Get(context.Background(), "work", dir)

	if calls != 2 {
		t.Errorf("made %d calls, want a refetch once the TTL passed", calls)
	}
}

// An account that was never signed in still has to render: the page says
// so in words rather than showing an empty card.
func TestServiceWithoutCredentials(t *testing.T) {
	snapshot := NewService().Get(context.Background(), "missing", filepath.Join(t.TempDir(), "nope"))
	if snapshot.State != StateSignedOut {
		t.Errorf("State = %q, want %q", snapshot.State, StateSignedOut)
	}
	if snapshot.Error == "" {
		t.Fatal("want an error explaining the login could not be read")
	}
	if snapshot.Usage != nil {
		t.Error("want no usage when there are no credentials")
	}
}

// The states are the whole point of the dot next to an account's name;
// each one has to come from a different real condition, or the dot is
// just decoration.
func TestServiceStates(t *testing.T) {
	future, past := time.Now().Add(30*24*time.Hour), time.Now().Add(-time.Hour)

	t.Run("linked", func(t *testing.T) {
		dir := t.TempDir()
		writeCredsFile(t, dir, time.Now().Add(time.Hour), future)
		srv := stubUsage(t, http.StatusOK, realPayload)
		got := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()}).
			Get(context.Background(), "a", dir)
		if got.State != StateLinked {
			t.Errorf("State = %q, want %q (error: %s)", got.State, StateLinked, got.Error)
		}
	})

	// The refresh token is the clock that actually ends a login, so this
	// is decided without asking the API at all.
	t.Run("expired refresh token", func(t *testing.T) {
		dir := t.TempDir()
		writeCredsFile(t, dir, past, past)
		srv := stubUsage(t, http.StatusOK, realPayload)
		got := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()}).
			Get(context.Background(), "a", dir)
		if got.State != StateExpired {
			t.Errorf("State = %q, want %q", got.State, StateExpired)
		}
	})

	// The bug this guards: Claude Code only refreshes the access token
	// when it runs, so between runs the stored one is stale while the
	// login is perfectly good. Sending it earned a 401 and the account
	// read as "login expired" — including the account the person was
	// using at that moment.
	t.Run("stale access token is still a linked account", func(t *testing.T) {
		dir := t.TempDir()
		writeCredsFile(t, dir, past, future)
		var called bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		got := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()}).
			Get(context.Background(), "a", dir)
		if got.State != StateLinked {
			t.Errorf("State = %q, want %q", got.State, StateLinked)
		}
		if called {
			t.Error("a token we already know is stale must not be sent")
		}
		if got.Error == "" {
			t.Error("want a note saying why the numbers are missing")
		}
		if got.Session == nil || got.Session.SessionExpiresAt == nil {
			t.Error("want the login clock, which is the one still running")
		}
	})

	t.Run("token rejected", func(t *testing.T) {
		dir := t.TempDir()
		writeCredsFile(t, dir, time.Now().Add(time.Hour), future)
		srv := stubUsage(t, http.StatusUnauthorized, "")
		got := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()}).
			Get(context.Background(), "a", dir)
		if got.State != StateExpired {
			t.Errorf("State = %q, want %q", got.State, StateExpired)
		}
	})

	// A server error says nothing about whether this account works, so
	// the page must not accuse it of being broken.
	t.Run("api unavailable", func(t *testing.T) {
		dir := t.TempDir()
		writeCredsFile(t, dir, time.Now().Add(time.Hour), future)
		srv := stubUsage(t, http.StatusInternalServerError, "")
		got := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()}).
			Get(context.Background(), "a", dir)
		if got.State != StateUnknown {
			t.Errorf("State = %q, want %q", got.State, StateUnknown)
		}
		if got.Session == nil {
			t.Error("want the session clock even when the API is down")
		}
	})
}

func stubUsage(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// When the API is unreachable the session clock is still known, and
// losing it would drop the answer to "how long am I signed in for?".
func TestServiceKeepsSessionWhenAPIFails(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, time.Now().Add(8*time.Hour), time.Now().Add(30*24*time.Hour))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	svc := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()})
	snapshot := svc.Get(context.Background(), "work", dir)

	if snapshot.Usage != nil {
		t.Error("want no usage when the token was rejected")
	}
	if snapshot.Error == "" {
		t.Error("want an error explaining why")
	}
	if snapshot.Session == nil || snapshot.Session.SessionExpiresAt == nil {
		t.Error("want the session clock even without usage")
	}
}
