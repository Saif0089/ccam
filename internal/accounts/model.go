// Package accounts is the source of truth for Claude Code account metadata.
// It knows nothing about PTYs, shells, or services — it just owns the
// account list and the on-disk directory each account maps to.
package accounts

import "time"

// Status is the lifecycle state of an account.
type Status string

const (
	// StatusPending means the account directory exists but no successful
	// login has been observed yet (login in progress, or never started).
	StatusPending Status = "pending"
	// StatusLinked means a login has been observed to succeed.
	StatusLinked Status = "linked"
)

// Account is one Claude Code identity, isolated via its own CLAUDE_CONFIG_DIR.
type Account struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	ConfigDir  string    `json:"configDir"`
	Alias      string    `json:"alias"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}
