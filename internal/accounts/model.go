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

// Isolation says which of Claude Code's two directory variables ccam
// exports for an account, and therefore how much of that account is
// separate from the rest.
type Isolation string

const (
	// IsolationConfigDir is the original scheme: CLAUDE_CONFIG_DIR
	// points at the account's own directory, so its login, sessions,
	// MCP servers, skills, plugins, hooks and projects/ transcripts are
	// all separate. It is also what an absent value means, so an
	// accounts.json written by an older ccam reads correctly.
	IsolationConfigDir Isolation = "config-dir"
	// IsolationCredentialsOnly exports CLAUDE_SECURESTORAGE_CONFIG_DIR
	// instead and leaves CLAUDE_CONFIG_DIR unset: only the credentials
	// come from the account's directory, and everything else — including
	// the projects/ transcripts — comes from the user's ~/.claude.
	//
	// This distinction is the reason the field exists rather than being
	// derived from Kind. A reader that attributes usage per account
	// (the Claude usage monitor does) must not look for transcripts
	// under ConfigDir for one of these rows: they are not there, and an
	// older accounts.json carries no way to tell.
	IsolationCredentialsOnly Isolation = "credentials-only"
)

// Account is one Claude Code identity.
//
// A managed account is scoped by its own directory; the default account
// is the opposite, reached by *removing* the variables, so its ConfigDir
// is deliberately empty. Those are not interchangeable — the credentials
// are keyed by that directory, and naming the default path explicitly
// reports logged out even when plain `claude` is logged in.
//
// What the directory scopes depends on Isolation. ConfigDir keeps its
// value either way, because it is the string hashed into the credential
// store's name: changing it would orphan the account's existing login.
type Account struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	Kind       Kind      `json:"kind"`
	ConfigDir  string    `json:"configDir"`
	Isolation  Isolation `json:"isolation"`
	Alias      string    `json:"alias"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// IsolationOrDefault reports the account's isolation, treating an absent
// or unrecognised value as the original config-dir scheme — which is what
// an accounts.json written before this field existed describes.
func (a Account) IsolationOrDefault() Isolation {
	if a.Isolation == IsolationCredentialsOnly {
		return IsolationCredentialsOnly
	}
	return IsolationConfigDir
}

// SharesUserConfigDir reports whether this account's sessions read and
// write the user's own ~/.claude rather than a private directory — and
// therefore whether its transcripts are pooled with every other such
// account's under ~/.claude/projects.
func (a Account) SharesUserConfigDir() bool {
	return a.IsolationOrDefault() == IsolationCredentialsOnly
}

// IsDefault reports whether this is the account plain `claude` uses.
func (a Account) IsDefault() bool { return a.Kind == KindDefault }

// OwnsConfigDir reports whether removing this account should delete its
// directory. Only ever true for a directory ccam created itself.
func (a Account) OwnsConfigDir() bool {
	return a.Kind != KindDefault && a.ConfigDir != ""
}
