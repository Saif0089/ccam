package panelpg

import (
	"context"
	"database/sql"
	"time"
)

// The metering tables live alongside the panel_state blob. Unlike the small,
// bounded panel data (kept as one JSON row so the panel logic stays unchanged),
// usage is high-volume and must be queried by person, model and time window — so
// it is real relational tables. person_id/account_id are the blob's string IDs;
// there are no foreign keys because those rows live in the JSON blob, and names
// are resolved from it when a board is drawn.
const usageDDL = `
CREATE TABLE IF NOT EXISTS usage_events (
    id                    bigserial PRIMARY KEY,
    at                    timestamptz NOT NULL DEFAULT now(),
    person_id             text NOT NULL DEFAULT '',
    account_id            text NOT NULL DEFAULT '',
    model                 text NOT NULL,
    input_tokens          bigint NOT NULL DEFAULT 0,
    output_tokens         bigint NOT NULL DEFAULT 0,
    cache_creation_tokens bigint NOT NULL DEFAULT 0,
    cache_read_tokens     bigint NOT NULL DEFAULT 0,
    weighted_tokens       double precision NOT NULL DEFAULT 0,
    cost_usd              double precision NOT NULL DEFAULT 0,
    request_id            text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS usage_events_at         ON usage_events(at);
CREATE INDEX IF NOT EXISTS usage_events_person_at  ON usage_events(person_id, at);
CREATE INDEX IF NOT EXISTS usage_events_account_at ON usage_events(account_id, at);

CREATE TABLE IF NOT EXISTS usage_counters (
    subject_type text NOT NULL,          -- 'person' | 'account'
    subject_id   text NOT NULL,
    window_start timestamptz NOT NULL,   -- truncated to the hour
    model        text NOT NULL,
    weighted_tokens       double precision NOT NULL DEFAULT 0,
    input_tokens          bigint NOT NULL DEFAULT 0,
    output_tokens         bigint NOT NULL DEFAULT 0,
    cache_creation_tokens bigint NOT NULL DEFAULT 0,
    cache_read_tokens     bigint NOT NULL DEFAULT 0,
    cost_usd              double precision NOT NULL DEFAULT 0,
    PRIMARY KEY (subject_type, subject_id, window_start, model)
);

CREATE TABLE IF NOT EXISTS limits (
    id                  text PRIMARY KEY,
    subject_type        text NOT NULL,           -- 'person' | 'account' | 'org'
    subject_id          text NOT NULL DEFAULT '', -- '' = org-wide
    window_kind         text NOT NULL,           -- 'day' | 'week' | 'month'
    max_weighted_tokens double precision,
    max_cost_usd        double precision,
    created_at          timestamptz NOT NULL DEFAULT now()
);`

func ensureUsageSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, usageDDL)
	return err
}

// UsageEvent is one forwarded response's metered usage, already priced (weighted
// tokens + USD) by the caller.
type UsageEvent struct {
	At            time.Time
	PersonID      string
	AccountID     string
	Model         string
	Input         int64
	Output        int64
	CacheCreation int64
	CacheRead     int64
	Weighted      float64
	CostUSD       float64
	RequestID     string
}

// RecordUsage stores one event and rolls it into the hourly counters for both
// the person and the account, in one transaction. It is best-effort from the
// gateway's point of view — the caller never lets a metering failure affect a
// forwarded request — but here it is transactional so a counter never drifts
// from the events it summarizes.
func (b *Backend) RecordUsage(ctx context.Context, ev UsageEvent) error {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	hour := ev.At.UTC().Truncate(time.Hour)

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO usage_events
		  (at, person_id, account_id, model, input_tokens, output_tokens,
		   cache_creation_tokens, cache_read_tokens, weighted_tokens, cost_usd, request_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		ev.At, ev.PersonID, ev.AccountID, ev.Model, ev.Input, ev.Output,
		ev.CacheCreation, ev.CacheRead, ev.Weighted, ev.CostUSD, ev.RequestID); err != nil {
		return err
	}

	for _, s := range []struct{ kind, id string }{
		{"person", ev.PersonID},
		{"account", ev.AccountID},
	} {
		if s.id == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_counters
			  (subject_type, subject_id, window_start, model, weighted_tokens,
			   input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, cost_usd)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (subject_type, subject_id, window_start, model) DO UPDATE SET
			  weighted_tokens       = usage_counters.weighted_tokens       + EXCLUDED.weighted_tokens,
			  input_tokens          = usage_counters.input_tokens          + EXCLUDED.input_tokens,
			  output_tokens         = usage_counters.output_tokens         + EXCLUDED.output_tokens,
			  cache_creation_tokens = usage_counters.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
			  cache_read_tokens     = usage_counters.cache_read_tokens     + EXCLUDED.cache_read_tokens,
			  cost_usd              = usage_counters.cost_usd              + EXCLUDED.cost_usd`,
			s.kind, s.id, hour, ev.Model, ev.Weighted,
			ev.Input, ev.Output, ev.CacheCreation, ev.CacheRead, ev.CostUSD); err != nil {
			return err
		}
	}
	return tx.Commit()
}
