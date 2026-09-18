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

// UsageReader is the metering read surface a board is drawn from. The Postgres
// backend implements it; a file-backed local panel has none, and the board
// routes are simply not mounted (nil).
type UsageReader interface {
	UsageBySubject(ctx context.Context, subjectType string, since time.Time) ([]SubjectUsage, error)
	HourlyTotals(ctx context.Context, subjectType, subjectID string, since time.Time) ([]HourBucket, error)
	LatestEventAt(ctx context.Context) (time.Time, error)
	// Quotas.
	ListLimits(ctx context.Context) ([]Limit, error)
	SetLimit(ctx context.Context, l Limit) error
	DeleteLimit(ctx context.Context, id string) error
	// Health: accounts whose shared login recently broke (used outside the gateway).
	RecentCollisions(ctx context.Context, since time.Time) (map[string]time.Time, error)
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

		rows, err := s.usage.UsageBySubject(ctx, subjectType, since)
		if err != nil {
			fail(w, http.StatusInternalServerError, "The usage board could not be read: "+err.Error())
			return
		}
		d, _ := s.store.Load() // names for the IDs; a read failure just leaves them blank

		subjects := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			subjects = append(subjects, map[string]any{
				"id":       row.SubjectID,
				"name":     s.subjectName(d, subjectType, row.SubjectID),
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

func normalizeWindow(name string) string {
	switch name {
	case "5h", "week", "month":
		return name
	default:
		return "day"
	}
}

func (s *Server) subjectName(d Data, subjectType, id string) string {
	if subjectType == "account" {
		for _, a := range d.Accounts {
			if a.ID == id {
				return a.Name
			}
		}
		return "removed account"
	}
	for _, p := range d.People {
		if p.ID == id {
			return p.Name
		}
	}
	return "removed member"
}
