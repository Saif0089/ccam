// Package panel is ccam's account-lending server: the small admin panel a
// person runs so the Claude logins a group shares can be handed out and taken
// back, instead of living on somebody's laptop for ever.
//
// Flat means flat. One admin, no teams, no roles, no organisations. The people
// it tracks are names attached to machines. The only rule the model enforces is
// that an account works on one machine at a time — not a policy choice but an
// OAuth one: Claude Code rotates its refresh token on every renewal, so two
// machines holding one login invalidate each other. That is the same mechanism
// that once merged three of this project's accounts into a single broken login.
package panel

import "time"

// Person is someone an account can be lent to.
type Person struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Device is one machine a person has enrolled. Each holds its own token, so a
// lost laptop can be cut off without disturbing the person's other machines.
type Device struct {
	ID         string    `json:"id"`
	PersonID   string    `json:"personId"`
	Name       string    `json:"name"`
	TokenHash  string    `json:"tokenHash"`
	EnrolledAt time.Time `json:"enrolledAt"`
	LastSeen   time.Time `json:"lastSeen,omitempty"`
}

// Account is one Claude login the panel lends out.
//
// Credential is the sealed login, present only once it has been signed in
// through the panel. It is sealed with the panel's own key (see keyfile.go) so
// that a copy of panel.json on its own is not a set of working logins.
type Account struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Email      string    `json:"email,omitempty"`
	Plan       string    `json:"plan,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	Credential []byte    `json:"credential,omitempty"`
}

// HasLogin reports whether this account has a login to lend.
func (a Account) HasLogin() bool { return len(a.Credential) > 0 }

// Assignment is one account lent to one person, for a while.
//
// It is never deleted. Taking an account back ends the assignment and leaves it
// in place, because "who had this, and when" is the question the activity log
// exists to answer and a deleted row cannot.
type Assignment struct {
	ID        string    `json:"id"`
	AccountID string    `json:"accountId"`
	PersonID  string    `json:"personId"`
	GrantedAt time.Time `json:"grantedAt"`
	// ExpiresAt zero means "until it is taken back".
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
	EndedWhy  string    `json:"endedWhy,omitempty"`
}

// Reasons an assignment ends. They are shown to the person who lost the
// account, so they are written as explanations rather than status codes.
const (
	EndedTakenBack  = "taken back"
	EndedExpired    = "expired"
	EndedReassigned = "given to someone else"
	EndedHandedBack = "handed back"
	EndedPersonGone = "the person was removed"
)

// Active reports whether this assignment is in force at now.
//
// Expiry is decided here rather than by a sweep, so an assignment that runs out
// while the panel is not running is not in force the moment it comes back. A
// background sweep would make the answer depend on whether anything happened to
// be looking.
func (a Assignment) Active(now time.Time) bool {
	switch {
	case !a.EndedAt.IsZero():
		return false
	case !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt):
		return false
	default:
		return true
	}
}

// Event is one line of the activity log.
//
// Who is written from the reader's point of view — the admin is "You" — because
// with a single admin the log is something one person reads about their own
// decisions, and "usama@example.com took Work back" is a stilted way to say it.
type Event struct {
	At   time.Time `json:"at"`
	Who  string    `json:"who"`
	What string    `json:"what"`
}

// Share is one account made available to one person through the gateway. Unlike
// the old one-holder assignment, an account can have many shares at once — that
// is the whole point of the gateway: many people, one login, together. Each
// share carries the hash of a gateway key the person's Claude Code presents; the
// gateway maps that key to this account's live token.
type Share struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	PersonID  string `json:"personId"`
	KeyHash   string `json:"keyHash"`
	// SealedKey is the gateway key, sealed with the panel key, so an enrolled
	// device can be handed it back on check-in. The gateway matches by KeyHash;
	// this is only for delivery. A gateway key is a scoped bearer token, not the
	// Claude credential, so this is a lower-stakes secret than the login itself.
	SealedKey []byte    `json:"sealedKey,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// JoinCode is a one-shot code that enrols a machine as a given person.
type JoinCode struct {
	CodeHash  string    `json:"codeHash"`
	PersonID  string    `json:"personId"`
	ExpiresAt time.Time `json:"expiresAt"`
	UsedAt    time.Time `json:"usedAt,omitempty"`
}

// Admin is the single administrator's password, salted and stretched.
type Admin struct {
	Salt []byte `json:"salt"`
	Hash []byte `json:"hash"`
}
