package panelpg

import (
	"context"
	"database/sql"
	"time"

	"clawdh/panel"
)

// account_windows holds each subscription's most recent real utilisation of its
// rolling 5h / weekly windows, as read from Anthropic's own rate-limit headers
// by the gateway. One row per account, overwritten each response — it is a
// snapshot ("42% of the weekly window right now"), not a time series.
const windowsDDL = `
CREATE TABLE IF NOT EXISTS account_windows (
    account_id   text PRIMARY KEY,
    fiveh_util   double precision NOT NULL DEFAULT 0,
    sevend_util  double precision NOT NULL DEFAULT 0,
    fiveh_reset  timestamptz,
    sevend_reset timestamptz,
    updated_at   timestamptz NOT NULL DEFAULT now()
);`

func ensureWindowsSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, windowsDDL)
	return err
}

// RecordWindows upserts an account's latest window utilisation. Best-effort and
// off the hot path: a write failure is swallowed (the gateway must never block a
// request on a metering write), and the previous reading simply stands.
func (b *Backend) RecordWindows(accountID string, fiveH, sevenD float64, fiveHReset, sevenDReset time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = b.db.ExecContext(ctx, `
		INSERT INTO account_windows (account_id, fiveh_util, sevend_util, fiveh_reset, sevend_reset, updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (account_id) DO UPDATE SET
		    fiveh_util   = EXCLUDED.fiveh_util,
		    sevend_util  = EXCLUDED.sevend_util,
		    fiveh_reset  = EXCLUDED.fiveh_reset,
		    sevend_reset = EXCLUDED.sevend_reset,
		    updated_at   = now()`,
		accountID, fiveH, sevenD, nullTime(fiveHReset), nullTime(sevenDReset))
}

// AccountWindows returns every account's latest window reading, most-recently-
// updated first. Names are filled by the handler from the panel blob.
func (b *Backend) AccountWindows(ctx context.Context) ([]panel.AccountWindow, error) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT account_id, fiveh_util, sevend_util, fiveh_reset, sevend_reset, updated_at
		  FROM account_windows ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []panel.AccountWindow
	for rows.Next() {
		var a panel.AccountWindow
		var fr, sr sql.NullTime
		if err := rows.Scan(&a.AccountID, &a.FiveH, &a.SevenD, &fr, &sr, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if fr.Valid {
			a.FiveHReset = fr.Time
		}
		if sr.Valid {
			a.SevenDReset = sr.Time
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
