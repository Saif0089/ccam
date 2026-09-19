package panel

import (
	"context"
	"net/http"
	"time"
)

// The usage boards. The metering data is written by the gateway and read here,
// so the panel that shows "who spent what, on which model" and the gateway that
// records it stay one system over one database.
//
// Every board carries an `asOf` (the time of the most recent metered event) and
// its `window`, so a viewer always knows how fresh a number is and nothing can
// look live while being stale — the failure the retired monitor made routine.

// ModelUsage is one model's totals within a window.
type ModelUsage struct {
	Model         string  `json:"model"`
	Weighted      float64 `json:"weighted"`
	Input         int64   `json:"input"`
	Output        int64   `json:"output"`
	CacheCreation int64   `json:"cacheCreation"`
	CacheRead     int64   `json:"cacheRead"`
	CostUSD       float64 `json:"costUsd"`
}

// SubjectUsage is one person's or account's usage in a window, broken down by
// model — the answer to "40% was this person; how much was Fable vs Opus vs
// Sonnet?" that the old app structurally could not give.
type SubjectUsage struct {
	SubjectID string       `json:"subjectId"`
	Weighted  float64      `json:"weighted"`
	CostUSD   float64      `json:"costUsd"`
	ByModel   []ModelUsage `json:"byModel"`
}

// HourBucket is one hour's total, for burn-rate lines and sparklines.
type HourBucket struct {
	Hour     time.Time `json:"hour"`
	Weighted float64   `json:"weighted"`
	CostUSD  float64   `json:"costUsd"`
}

// AccountWindow is a subscription's real utilisation of its rolling usage
// windows, read from Anthropic's own rate-limit headers by the gateway — the
// exact "% of the 5h / weekly window" that /usage shows, so a board can mirror
// it rather than estimate it. Fractions are 0..1.
type AccountWindow struct {
	AccountID   string    `json:"accountId"`
	Name        string    `json:"name"`
	FiveH       float64   `json:"fiveH"`
	SevenD      float64   `json:"sevenD"`
	FiveHReset  time.Time `json:"fiveHReset,omitempty"`
	SevenDReset time.Time `json:"sevenDReset,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// UsageReader is the metering read surface a board is drawn from. The Postgres
// backend implements it; a file-backed local panel has none, and the board
// routes are simply not mounted (nil).
type UsageReader interface {
	UsageBySubject(ctx context.Context, subjectType string, since time.Time) ([]SubjectUsage, error)
	// AccountUsageByPerson breaks one account's usage down by person — for the
	// "focus on this account" view.
	AccountUsageByPerson(ctx context.Context, accountID string, since time.Time) ([]SubjectUsage, error)
	HourlyTotals(ctx context.Context, subjectType, subjectID string, since time.Time) ([]HourBucket, error)
	LatestEventAt(ctx context.Context) (time.Time, error)
	// Quotas.
	ListLimits(ctx context.Context) ([]Limit, error)
	SetLimit(ctx context.Context, l Limit) error
	DeleteLimit(ctx context.Context, id string) error
	// LimitUsage is how much of one configured limit is used right now (0..1+),
	// and when its window resets — for the "82% — approaching" badge on the board.
	LimitUsage(ctx context.Context, l Limit) (fraction float64, resetAt time.Time, err error)
	// Health: accounts whose shared login recently broke (used outside the gateway).
	RecentCollisions(ctx context.Context, since time.Time) (map[string]time.Time, error)
	// AccountWindows is each account's latest real 5h / weekly utilisation, from
	// Anthropic's own headers — the exact numbers /usage draws its bars from.
	AccountWindows(ctx context.Context) ([]AccountWindow, error)
}

// windowSince maps a window name to a real time-based start (not a calendar-day
// sum, which is what made the old "5-hour" view actually cover a day or two).
func windowSince(now time.Time, name string) time.Time {
	switch name {
	case "5h":
		return now.Add(-5 * time.Hour)
	case "week":
		return now.Add(-7 * 24 * time.Hour)
	case "month":
		return now.Add(-30 * 24 * time.Hour)
	default: // "day"
		return now.Add(-24 * time.Hour)
	}
}

// handleUsage serves the per-person or per-account board for a window.
func (s *Server) handleUsage(subjectType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		windowName := r.URL.Query().Get("window")
		since := windowSince(s.now(), windowName)
		ctx := r.Context()

		// Focused on one account? Show its usage broken down by person instead of
		// every person across every account.
		var rows []SubjectUsage
		var err error
		if acct := r.URL.Query().Get("account"); acct != "" && subjectType == "person" {
			rows, err = s.usage.AccountUsageByPerson(ctx, acct, since)
		} else {
			rows, err = s.usage.UsageBySubject(ctx, subjectType, since)
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "The usage board could not be read: "+err.Error())
			return
		}
		d, _ := s.store.Load() // names for the IDs; a read failure just leaves them blank

		subjects := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			name, ok := s.lookupSubject(d, subjectType, row.SubjectID)
			if !ok {
				continue // a removed person/account — no ghost card
			}
			subjects = append(subjects, map[string]any{
				"id":       row.SubjectID,
				"name":     name,
				"weighted": row.Weighted,
				"costUsd":  row.CostUSD,
				"byModel":  row.ByModel,
			})
		}
		asOf, _ := s.usage.LatestEventAt(ctx)
		writeJSON(w, http.StatusOK, map[string]any{
			"window":   normalizeWindow(windowName),
			"since":    since.UTC(),
			"asOf":     asOf.UTC(),
			"subjects": subjects,
		})
	}
}

// handleBurn serves the per-hour time series for the burn-rate line, for one
// subject (id) or all of a kind.
func (s *Server) handleBurn(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	subjectType := q.Get("subject")
	if subjectType != "account" {
		subjectType = "person"
	}
	since := windowSince(s.now(), q.Get("window"))
	buckets, err := s.usage.HourlyTotals(r.Context(), subjectType, q.Get("id"), since)
	if err != nil {
		fail(w, http.StatusInternalServerError, "The burn-rate series could not be read: "+err.Error())
		return
	}
	asOf, _ := s.usage.LatestEventAt(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"window":  normalizeWindow(q.Get("window")),
		"asOf":    asOf.UTC(),
		"buckets": buckets,
	})
}

// handleWindows serves each account's real 5h / weekly utilisation — the same
// windows /usage shows, straight from Anthropic's headers.
func (s *Server) handleWindows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.usage.AccountWindows(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "The window utilisation could not be read: "+err.Error())
		return
	}
	d, _ := s.store.Load()
	// Only show windows for accounts that still exist. A removed account can leave
	// a stale row behind (the gateway captured its utilisation before it went), and
	// a "removed account" card is just confusing.
	kept := make([]AccountWindow, 0, len(rows))
	for _, row := range rows {
		name, ok := s.lookupSubject(d, "account", row.AccountID)
		if !ok {
			continue
		}
		row.Name = name
		kept = append(kept, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"windows": kept})
}

func normalizeWindow(name string) string {
	switch name {
	case "5h", "week", "month":
		return name
	default:
		return "day"
	}
}

func (s *Server) subjectName(d Data, subjectType, id string) string {
	if name, ok := s.lookupSubject(d, subjectType, id); ok {
		return name
	}
	if subjectType == "account" {
		return "removed account"
	}
	return "removed member"
}

// lookupSubject resolves an id to a current person or account name and reports
// whether it still exists — so a board can drop the rows of a subject that was
// removed instead of showing a ghost "removed account/member" card for data the
// gateway captured before it went away.
func (s *Server) lookupSubject(d Data, subjectType, id string) (string, bool) {
	if subjectType == "account" {
		for _, a := range d.Accounts {
			if a.ID == id {
				return a.Name, true
			}
		}
		return "", false
	}
	for _, p := range d.People {
		if p.ID == id {
			return p.Name, true
		}
	}
	return "", false
}
