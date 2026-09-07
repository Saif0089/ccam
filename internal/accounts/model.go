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

// Kind distinguishes accounts ccam created from the one it merely found.
type Kind string

const (
	// KindManaged is an account ccam created: it owns the directory,
	// generates an alias for it, and deletes the directory on removal.
	KindManaged Kind = "managed"
	// KindDefault is the account plain `claude` already uses — the one
	// with no CLAUDE_CONFIG_DIR override at all. ccam did not create it,
	// never writes an alias for it (typing `claude` is the alias), and
	// must never delete its directory: that is the user's main login.
	KindDefault Kind = "default"
)

// Account is one Claude Code identity.
//
// A managed account is isolated via its own CLAUDE_CONFIG_DIR. The
// default account is the opposite: it is reached by *removing* that
// variable, so its ConfigDir is deliberately empty. Those are not
// interchangeable — on macOS the credentials are keyed by config dir,
// and pointing CLAUDE_CONFIG_DIR at the default path reports logged
// out even when plain `claude` is logged in.
type Account struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	Kind       Kind      `json:"kind"`
	ConfigDir  string    `json:"configDir"`
	Alias      string    `json:"alias"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// IsDefault reports whether this is the account plain `claude` uses.
func (a Account) IsDefault() bool { return a.Kind == KindDefault }

// OwnsConfigDir reports whether removing this account should delete its
// directory. Only ever true for a directory ccam created itself.
func (a Account) OwnsConfigDir() bool {
	return a.Kind != KindDefault && a.ConfigDir != ""
}
