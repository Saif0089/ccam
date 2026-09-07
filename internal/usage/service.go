package usage

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"time"

	"ccam/internal/config"
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

// TTL is how long a fetched report is reused.
//
// The page polls every few seconds and never asks anyone to press a
// refresh button, so this is what decides how often that polling
// actually reaches Anthropic: a dozen page polls share one upstream
// call. This endpoint is not a documented API and publishes no
// rate-limit budget of any kind, and a shorter TTL here — four calls a
// minute per account — was enough to earn a 429 in an afternoon. One a
// minute per account is the rate a page left open all day can hold.
const TTL = time.Minute

// Rate-limit backoff. Nothing tells us what the limit is, so a refusal
// is answered by waiting longer each time rather than by guessing a
// safe rate: a minute, then two, four, eight, capped at fifteen. One
// success clears it.
const (
	backoffFirst = time.Minute
	backoffMax   = 15 * time.Minute
)

// cooldown is how long one account is not asking Anthropic anything.
type cooldown struct {
	until time.Time
	step  time.Duration
}

type cacheEntry struct {
	snapshot Snapshot
	at       time.Time
}

// load is one in-flight fetch for one account, which every request that
// arrives while it runs waits on instead of starting its own.
type load struct {
	done     chan struct{}
	snapshot Snapshot
}

// Service caches usage per account.
type Service struct {
	client *Client
	now    func() time.Time

	// CachePath overrides where the last good numbers are kept, for
	// tests. Empty means ~/.ccam/usage.json.
	CachePath string

	mu       sync.Mutex
	entries  map[string]cacheEntry
	inflight map[string]*load
	// cooldowns holds off accounts Anthropic has refused; lastReport
	// keeps the numbers they had when it did, because a rate limit is a
	// reason to stop asking, not a reason to blank a card that was
	// showing something true a minute ago.
	cooldowns  map[string]cooldown
	lastReport map[string]*Report
}

// NewService returns a Service using the default endpoint, keeping its
// last good numbers under ~/.ccam so a restart does not lose them.
func NewService() *Service {
	s := NewServiceWithClient(NewClient())
	if path, err := config.UsageCacheFile(); err == nil {
		s.CachePath = path
		s.loadReports()
	}
	return s
}

// NewServiceWithClient returns a Service using a specific client, for
// tests.
func NewServiceWithClient(c *Client) *Service {
	// No CachePath: nothing is read from or written to disk. Only the
	// running service sets one, so a test constructing a Service can
	// never reach into the real ~/.ccam — which it did, once.
	return &Service{
		client:     c,
		now:        time.Now,
		entries:    map[string]cacheEntry{},
		inflight:   map[string]*load{},
		cooldowns:  map[string]cooldown{},
		lastReport: map[string]*Report{},
	}
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
	// One fetch per account at a time. The page polls every few seconds
	// and a second tab, or a slow answer from Anthropic, would otherwise
	// have every overlapping request start its own upstream call for the
	// same numbers.
	if pending, ok := s.inflight[accountID]; ok {
		s.mu.Unlock()
		select {
		case <-pending.done:
			return pending.snapshot
		case <-ctx.Done():
			return Snapshot{State: StateUnknown, Error: "Reading plan usage was interrupted."}
		}
	}
	pending := &load{done: make(chan struct{})}
	s.inflight[accountID] = pending
	s.mu.Unlock()

	snapshot := s.fetch(ctx, accountID, configDir)

	s.mu.Lock()
	// A request the browser gave up on (a reload, a closed tab) cancels
	// its context mid-fetch. That failure says nothing about the
	// account, and caching it would blame it for the next TTL.
	if ctx.Err() == nil {
		s.entries[accountID] = cacheEntry{snapshot: snapshot, at: s.now()}
	}
	delete(s.inflight, accountID)
	s.mu.Unlock()

	pending.snapshot = snapshot
	close(pending.done)
	return snapshot
}

// Forget drops an account's cached snapshot, after it is removed or
// reconnected.
func (s *Service) Forget(accountID string) {
	s.mu.Lock()
	delete(s.entries, accountID)
	// The saved numbers go too. Every caller means "what ccam knows
	// about this account no longer applies" — it was removed, or signed
	// in as someone else — and numbers that outlived that would come
	// back under an account they were never about.
	delete(s.lastReport, accountID)
	delete(s.cooldowns, accountID)
	s.mu.Unlock()
	s.saveReports()
}

// waiting reports how long this account is still holding off, and the
// numbers it last managed to read.
func (s *Service) waiting(accountID string) (time.Time, *Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until := time.Time{}
	if c, ok := s.cooldowns[accountID]; ok && s.now().Before(c.until) {
		until = c.until
	}
	return until, s.lastReport[accountID]
}

// refused starts or lengthens an account's cooldown after a rate limit,
// and returns when it may ask again.
func (s *Service) refused(accountID string, retryAfter time.Duration) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	step := s.cooldowns[accountID].step
	if step <= 0 {
		step = backoffFirst
	} else if step < backoffMax {
		step *= 2
	}
	if step > backoffMax {
		step = backoffMax
	}
	// A Retry-After worth waiting wins, but never shortens the backoff:
	// the server has already said no more than it is willing to say.
	if retryAfter > step {
		step = retryAfter
	}
	until := s.now().Add(step)
	s.cooldowns[accountID] = cooldown{until: until, step: step}
	return until
}

// allowed clears an account's cooldown after a successful read and
// keeps the numbers for the next time one is refused.
func (s *Service) allowed(accountID string, report *Report) {
	s.mu.Lock()
	delete(s.cooldowns, accountID)
	changed := false
	if report != nil {
		s.lastReport[accountID] = report
		changed = true
	}
	s.mu.Unlock()

	if changed {
		s.saveReports()
	}
}

func (s *Service) fetch(ctx context.Context, accountID, configDir string) Snapshot {
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

	// Still serving a rate limit: say so, keep the last numbers on
	// screen, and ask nobody anything.
	if until, last := s.waiting(accountID); !until.IsZero() {
		snapshot.Usage = last
		snapshot.Error = pausedNote(until, last)
		return snapshot
	}

	report, err := s.client.Fetch(ctx, creds)
	if err != nil {
		// The session clock is still worth showing even when the plan
		// numbers aren't: it answers a different question.
		snapshot.Error = err.Error()

		var limited *RateLimited
		switch {
		case errors.Is(err, ErrLoginRejected):
			snapshot.State = StateExpired
		case errors.As(err, &limited):
			// A refusal for asking too often says the login is fine —
			// it was accepted and then throttled. Leave the account
			// linked, keep whatever numbers it last had, and wait.
			_, last := s.waiting(accountID)
			snapshot.Usage = last
			snapshot.Error = pausedNote(s.refused(accountID, limited.RetryAfter), last)
		default:
			// Reaching Anthropic failed, which says nothing about
			// whether this account works. Don't claim it is broken.
			snapshot.State = StateUnknown
		}
		return snapshot
	}
	s.allowed(accountID, report)
	snapshot.Usage = report
	return snapshot
}

// pausedNote is what the card says while an account is waiting out a
// rate limit: what happened, how old the numbers under it are, and when
// ccam will try again.
func pausedNote(until time.Time, last *Report) string {
	when := "No numbers have been read yet"
	if last != nil && !last.FetchedAt.IsZero() {
		when = "These numbers are from " + last.FetchedAt.Local().Format("15:04")
	}
	return "Anthropic is rate-limiting plan usage. " + when +
		"; trying again at " + until.Local().Format("15:04") + "."
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
