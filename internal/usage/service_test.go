package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// The page polls every few seconds and a second tab doubles that, so
// overlapping requests for one account must share a single call to
// Anthropic rather than each starting their own.
func TestOverlappingRequestsShareOneFetch(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, time.Now().Add(8*time.Hour), time.Now().Add(30*24*time.Hour))

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Long enough that every caller below arrives while it runs.
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(realPayload))
	}))
	defer srv.Close()

	svc := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()})

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := svc.Get(context.Background(), "work", dir); got.Usage == nil {
				t.Errorf("no usage returned: %s", got.Error)
			}
		}()
		time.Sleep(20 * time.Millisecond)
	}
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Errorf("made %d upstream calls for one cache miss, want 1", n)
	}
}

// A request the browser abandoned must not leave its failure in the
// cache for everyone who asks next.
func TestACancelledRequestIsNotCached(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, time.Now().Add(8*time.Hour), time.Now().Add(30*24*time.Hour))

	release := make(chan struct{})
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release
		_, _ = w.Write([]byte(realPayload))
	}))
	defer srv.Close()
	defer close(release)

	svc := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Snapshot, 1)
	go func() { done <- svc.Get(ctx, "work", dir) }()
	time.Sleep(100 * time.Millisecond)
	cancel()

	if got := <-done; got.Usage != nil {
		t.Fatal("want no usage from the abandoned request")
	}

	svc.mu.Lock()
	_, cached := svc.entries["work"]
	svc.mu.Unlock()
	if cached {
		t.Error("the abandoned request's failure was cached")
	}
}

// A 429 is not a broken account and not a reason to blank the card. It
// is a reason to stop asking for a while — which is the part that has
// to be tested, because the wrong answer here is an endless retry loop
// that keeps the rate limit alive.
func TestRateLimitBacksOffAndKeepsTheLastNumbers(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, time.Now().Add(8*time.Hour), time.Now().Add(30*24*time.Hour))

	var calls atomic.Int64
	limited := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if limited.Load() {
			w.Header().Set("Retry-After", "0") // what this endpoint actually sends
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(realPayload))
	}))
	defer srv.Close()

	svc := NewServiceWithClient(&Client{Endpoint: srv.URL, HTTPClient: srv.Client()})
	now := time.Now()
	svc.now = func() time.Time { return now }

	// A good read first, so there is something to keep showing.
	if got := svc.Get(context.Background(), "work", dir); got.Usage == nil {
		t.Fatalf("no usage on the first read: %s", got.Error)
	}

	limited.Store(true)
	now = now.Add(TTL + time.Second)
	got := svc.Get(context.Background(), "work", dir)

	if got.State != StateLinked {
		t.Errorf("State = %q, want %q: being throttled says the login works", got.State, StateLinked)
	}
	if got.Usage == nil {
		t.Error("want the last numbers kept on screen rather than an empty card")
	}
	if !strings.Contains(got.Error, "rate-limiting") {
		t.Errorf("note = %q, want it to say what happened", got.Error)
	}

	// Now the part that matters: retries follow the backoff, not the
	// cache. Ten cache misses over ten minutes must not be ten more
	// requests — the backoff schedule (1, 2, 4, 8 minutes) allows about
	// four, and every poll in between is answered without asking.
	before := calls.Load()
	for i := 0; i < 10; i++ {
		now = now.Add(TTL)
		svc.Get(context.Background(), "work", dir)
	}
	if after := calls.Load() - before; after > 5 {
		t.Errorf("made %d requests across 10 cache misses, want the backoff to allow ~4", after)
	}

	// Once the current wait expires, it tries again — and a success
	// clears the backoff.
	limited.Store(false)
	now = now.Add(backoffMax + time.Second)
	if got := svc.Get(context.Background(), "work", dir); got.Usage == nil || got.Error != "" {
		t.Errorf("want a clean read once the backoff expired, got error %q", got.Error)
	}
	svc.mu.Lock()
	_, stillWaiting := svc.cooldowns["work"]
	svc.mu.Unlock()
	if stillWaiting {
		t.Error("a successful read must clear the backoff")
	}
}

// Each refusal waits longer than the last, so a limit that is not
// letting up is not met with the same request rate forever.
func TestBackoffDoubles(t *testing.T) {
	svc := NewServiceWithClient(&Client{})
	now := time.Now()
	svc.now = func() time.Time { return now }

	want := []time.Duration{backoffFirst, 2 * backoffFirst, 4 * backoffFirst, 8 * backoffFirst, backoffMax, backoffMax}
	for i, expected := range want {
		until := svc.refused("work", 0)
		if got := until.Sub(now); got != expected {
			t.Errorf("refusal %d waits %s, want %s", i+1, got, expected)
		}
	}
}

// A Retry-After worth waiting is honoured; the "0" this endpoint sends
// is not a delay and must not shorten the backoff.
func TestRetryAfterHeader(t *testing.T) {
	if got := retryAfter("120"); got != 2*time.Minute {
		t.Errorf("retryAfter(\"120\") = %s, want 2m", got)
	}
	if got := retryAfter("0"); got != 0 {
		t.Errorf("retryAfter(\"0\") = %s, want 0", got)
	}
	if got := retryAfter(""); got != 0 {
		t.Errorf("retryAfter(\"\") = %s, want 0", got)
	}
	if got := retryAfter(time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)); got < time.Minute {
		t.Errorf("an HTTP-date Retry-After gave %s, want about 90s", got)
	}

	svc := NewServiceWithClient(&Client{})
	now := time.Now()
	svc.now = func() time.Time { return now }
	if got := svc.refused("work", 10*time.Minute).Sub(now); got != 10*time.Minute {
		t.Errorf("a 10m Retry-After produced %s, want it honoured", got)
	}
	// ...but it never shortens what the backoff already decided.
	svc2 := NewServiceWithClient(&Client{})
	svc2.now = func() time.Time { return now }
	if got := svc2.refused("work", time.Second).Sub(now); got != backoffFirst {
		t.Errorf("a 1s Retry-After produced %s, want the %s backoff to win", got, backoffFirst)
	}
}
