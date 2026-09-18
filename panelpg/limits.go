package panelpg

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// newHexID mints a short random id, matching the shape of the panel's own ids.
func newHexID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Quotas. A limit caps a person (or the whole org) to a number of weighted
// tokens and/or a USD amount within a calendar window (day/week/month, UTC
// boundaries — the reset shape Anthropic's own spend limits use). The gateway
// checks the tightest applicable limit before serving; over it, the member gets
// a definitive 429 with the window's reset time, exactly like a real spend cap.

// Limit is one configured quota.
type Limit struct {
	ID          string   `json:"id"`
	SubjectType string   `json:"subjectType"` // 'person' | 'org'
	SubjectID   string   `json:"subjectId"`   // '' for org-wide
	WindowKind  string   `json:"windowKind"`  // 'day' | 'week' | 'month'
	MaxWeighted *float64 `json:"maxWeighted,omitempty"`
	MaxCostUSD  *float64 `json:"maxCostUsd,omitempty"`
}

// LimitStatus is a subject's standing against the tightest quota that applies:
// how much of it is used (0..1+), when it resets, and a message for the 429 or a
// 75%/95% warning. Over is true once any applicable limit is reached.
type LimitStatus struct {
	Over     bool      `json:"over"`
	Fraction float64   `json:"fraction"`
	ResetAt  time.Time `json:"resetAt"`
	Message  string    `json:"message"`
}

// windowBounds returns the calendar start and reset of a quota window in UTC:
// day = midnight, week = Monday, month = the 1st.
func windowBounds(now time.Time, kind string) (start, reset time.Time) {
	n := now.UTC()
	switch kind {
	case "week":
		off := (int(n.Weekday()) + 6) % 7 // days since Monday
		start = time.Date(n.Year(), n.Month(), n.Day()-off, 0, 0, 0, 0, time.UTC)
		reset = start.AddDate(0, 0, 7)
	case "month":
		start = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
		reset = start.AddDate(0, 1, 0)
	default: // day
		start = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
		reset = start.AddDate(0, 0, 1)
	}
	return
}

// PersonLimitStatus checks a person against their own limits and any org-wide
// limit, returning the tightest (most-used) one. now is a parameter for tests.
// A person with no applicable limit comes back Over=false, Fraction=0.
func (b *Backend) PersonLimitStatus(ctx context.Context, personID string, now time.Time) (LimitStatus, error) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT subject_type, window_kind, max_weighted_tokens, max_cost_usd
		  FROM limits
		 WHERE (subject_type = 'person' AND subject_id = $1) OR subject_type = 'org'`, personID)
	if err != nil {
		return LimitStatus{}, err
	}
	defer rows.Close()

	type lim struct {
		subjectType, windowKind string
		maxW, maxC              sql.NullFloat64
	}
	var limits []lim
	for rows.Next() {
		var l lim
		if err := rows.Scan(&l.subjectType, &l.windowKind, &l.maxW, &l.maxC); err != nil {
			return LimitStatus{}, err
		}
		limits = append(limits, l)
	}
	if err := rows.Err(); err != nil {
		return LimitStatus{}, err
	}

	var tightest LimitStatus
	for _, l := range limits {
		start, reset := windowBounds(now, l.windowKind)
		usedW, usedC, err := b.usedSince(ctx, l.subjectType, personID, start)
		if err != nil {
			return LimitStatus{}, err
		}
		frac := 0.0
		if l.maxW.Valid && l.maxW.Float64 > 0 {
			frac = maxf(frac, usedW/l.maxW.Float64)
		}
		if l.maxC.Valid && l.maxC.Float64 > 0 {
			frac = maxf(frac, usedC/l.maxC.Float64)
		}
		if frac > tightest.Fraction {
			who := "your"
			if l.subjectType == "org" {
				who = "the team's"
			}
			tightest = LimitStatus{
				Over:     frac >= 1.0,
				Fraction: frac,
				ResetAt:  reset,
				Message: fmt.Sprintf("%s clawdh %s quota is reached; it resets %s UTC.",
					who, l.windowKind, reset.Format("2006-01-02 15:04")),
			}
		}
	}
	return tightest, nil
}

// usedSince sums a person's (or, for an org limit, all people's) weighted tokens
// and USD in a window.
func (b *Backend) usedSince(ctx context.Context, subjectType, personID string, start time.Time) (weighted, cost float64, err error) {
	q := `SELECT COALESCE(SUM(weighted_tokens),0), COALESCE(SUM(cost_usd),0)
	        FROM usage_counters WHERE subject_type = 'person' AND window_start >= $1`
	args := []any{start}
	if subjectType != "org" {
		q += ` AND subject_id = $2`
		args = append(args, personID)
	}
	err = b.db.QueryRowContext(ctx, q, args...).Scan(&weighted, &cost)
	return
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// SetLimit creates or replaces a limit. id "" makes a new one; a repeated
// (subject_type, subject_id, window_kind) replaces the prior cap for that pair.
func (b *Backend) SetLimit(ctx context.Context, l Limit) error {
	if l.ID == "" {
		l.ID = newHexID()
	}
	_, err := b.db.ExecContext(ctx, `
		INSERT INTO limits (id, subject_type, subject_id, window_kind, max_weighted_tokens, max_cost_usd)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		l.ID, l.SubjectType, l.SubjectID, l.WindowKind, nullF(l.MaxWeighted), nullF(l.MaxCostUSD))
	return err
}

// ListLimits returns every configured limit.
func (b *Backend) ListLimits(ctx context.Context) ([]Limit, error) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT id, subject_type, subject_id, window_kind, max_weighted_tokens, max_cost_usd
		  FROM limits ORDER BY subject_type, subject_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Limit
	for rows.Next() {
		var l Limit
		var w, c sql.NullFloat64
		if err := rows.Scan(&l.ID, &l.SubjectType, &l.SubjectID, &l.WindowKind, &w, &c); err != nil {
			return nil, err
		}
		if w.Valid {
			l.MaxWeighted = &w.Float64
		}
		if c.Valid {
			l.MaxCostUSD = &c.Float64
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteLimit removes a limit by id.
func (b *Backend) DeleteLimit(ctx context.Context, id string) error {
	_, err := b.db.ExecContext(ctx, `DELETE FROM limits WHERE id = $1`, id)
	return err
}

func nullF(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
