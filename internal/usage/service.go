package usage

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"time"
)

// Snapshot is what the page shows for one account: whether the login
// still works, how much of the plan is used, and how long the login
// itself has left.
type Snapshot struct {
	// State is the account's live condition, decided here rather than
	// guessed at from the error text in the browser.
	State State `json:"state"`

	// Usage is nil when plan usage could not be read; Error then says
	// why, in words meant for the person reading the page.
	Usage *Report `json:"usage,omitempty"`
	Error string  `json:"error,omitempty"`

	// Session describes the login rather than the plan.
	Session *SessionInfo `json:"session,omitempty"`
}

// State is what the dot next to an account's name means.
type State string

const (
	// StateLinked: this account will work if you run it now.
	StateLinked State = "linked"
	// StateSignedOut: nobody has signed this account in.
	StateSignedOut State = "signed-out"
	// StateExpired: there is a login, but it is no longer accepted.
	StateExpired State = "expired"
	// StateUnknown: ccam could not tell — usually no network.
	StateUnknown State = "unknown"
)

// SessionInfo answers "how long am I signed in for?".
//
// There are two clocks, and conflating them is what makes the question
// confusing: the access token is short-lived and Claude Code refreshes
// it in the background without anyone noticing, while the refresh token
// is what actually ends the login when it expires.
type SessionInfo struct {
	AccessExpiresAt  *time.Time `json:"accessExpiresAt,omitempty"`
	SessionExpiresAt *time.Time `json:"sessionExpiresAt,omitempty"`
	Plan             string     `json:"plan,omitempty"`
	RateLimitTier    string     `json:"rateLimitTier,omitempty"`
}

// TTL is how long a fetched report is reused: long enough that opening
// the page repeatedly doesn't hammer the API, short enough that the
// numbers still feel live.
const TTL = 60 * time.Second

type cacheEntry struct {
	snapshot Snapshot
	at       time.Time
}

// Service caches usage per account.
type Service struct {
	client *Client
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

// NewService returns a Service using the default endpoint.
func NewService() *Service {
	return NewServiceWithClient(NewClient())
}

// NewServiceWithClient returns a Service using a specific client, for
// tests.
func NewServiceWithClient(c *Client) *Service {
	return &Service{client: c, now: time.Now, entries: map[string]cacheEntry{}}
}

// Get returns the snapshot for one account, fetching it when the cached
// one has expired. accountID only names the cache slot; configDir is
// what identifies the account to Claude Code (empty for the default).
func (s *Service) Get(ctx context.Context, accountID, configDir string) Snapshot {
	s.mu.Lock()
	if entry, ok := s.entries[accountID]; ok && s.now().Sub(entry.at) < TTL {
		s.mu.Unlock()
		return entry.snapshot
	}
	s.mu.Unlock()

	snapshot := s.load(ctx, configDir)

	s.mu.Lock()
	s.entries[accountID] = cacheEntry{snapshot: snapshot, at: s.now()}
	s.mu.Unlock()
	return snapshot
}

// Forget drops an account's cached snapshot, after it is removed or
// reconnected.
func (s *Service) Forget(accountID string) {
	s.mu.Lock()
	delete(s.entries, accountID)
	s.mu.Unlock()
}

func (s *Service) load(ctx context.Context, configDir string) Snapshot {
	creds, err := ReadCredentials(configDir)
	switch {
	case errors.Is(err, errNoLogin), errors.Is(err, fs.ErrNotExist):
		return Snapshot{
			State: StateSignedOut,
			Error: "Not signed in yet. Use Connect to link a Claude login.",
		}
	case err != nil:
		return Snapshot{
			State: StateUnknown,
			Error: "Could not read this account's login: " + err.Error(),
		}
	}

	snapshot := Snapshot{State: StateLinked, Session: sessionInfo(creds)}

	// A refresh token that has run out is the one case where the login
	// really is over; the short-lived access token expiring is routine.
	if !creds.RefreshExpiresAt.IsZero() && s.now().After(creds.RefreshExpiresAt) {
		snapshot.State = StateExpired
		snapshot.Error = "This login has expired. Reconnect the account to use it again."
		return snapshot
	}

	// The stored access token is short-lived and only Claude Code
	// refreshes it — it does that when it runs, not on a timer. Between
	// runs the stored one goes stale while the login itself is fine, so
	// sending it would earn a 401 and make a working account read as
	// rejected. Say what is actually true instead: linked, numbers
	// pending the next run.
	if !creds.ExpiresAt.IsZero() && !s.now().Before(creds.ExpiresAt) {
		snapshot.Error = "Plan usage will show again once Claude Code refreshes this account's token — run it once."
		return snapshot
	}

	report, err := s.client.Fetch(ctx, creds)
	if err != nil {
		// The session clock is still worth showing even when the plan
		// numbers aren't: it answers a different question.
		snapshot.Error = err.Error()
		if errors.Is(err, ErrLoginRejected) {
			snapshot.State = StateExpired
		} else {
			// Reaching Anthropic failed, which says nothing about
			// whether this account works. Don't claim it is broken.
			snapshot.State = StateUnknown
		}
		return snapshot
	}
	snapshot.Usage = report
	return snapshot
}

func sessionInfo(creds Credentials) *SessionInfo {
	info := &SessionInfo{
		Plan:          creds.SubscriptionType,
		RateLimitTier: creds.RateLimitTier,
	}
	if !creds.ExpiresAt.IsZero() {
		t := creds.ExpiresAt.UTC()
		info.AccessExpiresAt = &t
	}
	if !creds.RefreshExpiresAt.IsZero() {
		t := creds.RefreshExpiresAt.UTC()
		info.SessionExpiresAt = &t
	}
	return info
}
