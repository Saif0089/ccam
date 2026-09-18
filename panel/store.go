package panel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// maxActivity caps the activity log. It is the only unbounded thing here, and
// a check-in every thirty seconds per machine would otherwise grow the file
// for ever. Check-ins are not logged for that reason; only decisions are.
const maxActivity = 2000

// ErrAccountBusy is returned when an account is asked for while somebody else
// has it and the caller did not say to move it.
var ErrAccountBusy = errors.New("that account is with someone else")

// Data is the whole panel, as it sits on disk.
//
// It is one JSON file rather than a database on purpose. This holds tens of
// rows, is written by exactly one process, and ccam already keeps its accounts
// this way (internal/accounts/store.go); a SQL engine would be a megabytes-long
// dependency and a second way of doing the same thing. The invariants a
// database would enforce with a unique index are enforced here by holding the
// mutex across read-decide-write, which is the same guarantee for one writer.
type Data struct {
	Admin       *Admin       `json:"admin,omitempty"`
	People      []Person     `json:"people,omitempty"`
	Devices     []Device     `json:"devices,omitempty"`
	Accounts    []Account    `json:"accounts,omitempty"`
	Assignments []Assignment `json:"assignments,omitempty"`
	JoinCodes   []JoinCode   `json:"joinCodes,omitempty"`
	Shares      []Share      `json:"shares,omitempty"`
	Activity    []Event      `json:"activity,omitempty"`
}

// Store is the panel's persistence. Every mutation takes the lock, re-reads the
// current state, decides, and writes it back — atomically for a file, and with
// a compare-and-swap for a shared database, so two writers can never both
// believe they won.
//
// The one-holder rule lives in that read-decide-write being indivisible. On a
// single machine the mutex makes it so. On several machines behind one database
// — a panel deployed to a serverless host, where each request may be a fresh
// process — the mutex cannot, so the write is conditional on nothing having
// changed since the read, and a lost race simply runs the decision again
// against the winner's result. Either way, two people are never handed the same
// account: the second attempt sees the first assignment and refuses.
type Store struct {
	mu      sync.Mutex
	backend Backend
	now     func() time.Time
}

// Backend is where a Store keeps its one blob of state. Load returns the
// current bytes and a version; Save writes new bytes only if the version has
// not moved, reporting whether it won. A brand-new store loads as (nil, 0).
//
// It is exported so a Postgres implementation can live in its own package,
// keeping that driver out of the ccam client binary that ships to every machine.
type Backend interface {
	Load() (raw []byte, version int64, err error)
	Save(raw []byte, expected int64) (ok bool, err error)
}

// NewStore returns a Store kept in one JSON file, which need not exist yet.
// This is what `ccam panel serve` uses: one machine, one writer.
func NewStore(path string) *Store {
	return &Store{backend: &fileBackend{path: path}, now: time.Now}
}

// NewStoreWithBackend returns a Store over any backend — a database, for a
// panel that runs as more than one process at once.
func NewStoreWithBackend(b Backend) *Store {
	return &Store{backend: b, now: time.Now}
}

// maxCASRetries bounds how many times a lost compare-and-swap is retried before
// giving up. Contention is a handful of admin clicks and a check-in every
// thirty seconds per machine, so a real collision is rare and clears in one
// retry; this only stops a pathological livelock.
const maxCASRetries = 8

// Load reads the panel. A missing file is an empty panel, not an error: that is
// what a machine looks like before anyone has set it up.
func (s *Store) Load() (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (Data, error) {
	d, _, err := s.readLocked()
	return d, err
}

// readLocked returns the current panel and the version to write back against.
func (s *Store) readLocked() (Data, int64, error) {
	var d Data
	raw, version, err := s.backend.Load()
	if err != nil {
		return d, 0, err
	}
	if len(raw) == 0 {
		return d, version, nil // never set up yet
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, 0, fmt.Errorf("reading the panel state: %w", err)
	}
	return d, version, nil
}

// Mutate runs fn against the current panel and writes the result. fn returning
// an error writes nothing.
//
// Expiry is settled before fn runs, so every caller — a check-in, an admin
// click, a listing — sees the same panel, and an assignment that ran out while
// nothing was looking is already ended rather than ending at the moment someone
// happens to ask.
func (s *Store) Mutate(fn func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for attempt := 0; ; attempt++ {
		d, version, err := s.readLocked()
		if err != nil {
			return err
		}
		s.sealExpired(&d)
		if err := fn(&d); err != nil {
			return err
		}
		ok, err := s.saveVersioned(&d, version)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		// Someone else wrote between our read and our write. Their change is
		// now the truth — including any assignment they just made — so run the
		// whole decision again against it. fn re-checks the invariant, so a
		// second assignment of the same account still refuses.
		if attempt >= maxCASRetries {
			return errors.New("the panel is being changed by too many people at once; try again")
		}
	}
}

// sealExpired ends assignments whose time has run out, and says so in the log.
func (s *Store) sealExpired(d *Data) {
	now := s.now()
	for i := range d.Assignments {
		a := &d.Assignments[i]
		if !a.EndedAt.IsZero() || a.ExpiresAt.IsZero() || now.Before(a.ExpiresAt) {
			continue
		}
		a.EndedAt = a.ExpiresAt
		a.EndedWhy = EndedExpired
		d.log(a.ExpiresAt, "System", fmt.Sprintf("%s ran out and went back to the pool, from %s",
			d.accountName(a.AccountID), d.personName(a.PersonID)))
	}
}

func (s *Store) saveVersioned(d *Data, version int64) (bool, error) {
	if len(d.Activity) > maxActivity {
		d.Activity = d.Activity[len(d.Activity)-maxActivity:]
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return false, err
	}
	return s.backend.Save(raw, version)
}

// ---------------------------------------------------------------- lookups

func (d *Data) accountName(id string) string {
	for _, a := range d.Accounts {
		if a.ID == id {
			return a.Name
		}
	}
	return "an account that no longer exists"
}

func (d *Data) personName(id string) string {
	for _, p := range d.People {
		if p.ID == id {
			return p.Name
		}
	}
	return "someone who is no longer here"
}

// Account returns an account by id.
func (d *Data) Account(id string) (*Account, bool) {
	for i := range d.Accounts {
		if d.Accounts[i].ID == id {
			return &d.Accounts[i], true
		}
	}
	return nil, false
}

// Person returns a person by id.
func (d *Data) Person(id string) (*Person, bool) {
	for i := range d.People {
		if d.People[i].ID == id {
			return &d.People[i], true
		}
	}
	return nil, false
}

// Device returns a device by id.
func (d *Data) Device(id string) (*Device, bool) {
	for i := range d.Devices {
		if d.Devices[i].ID == id {
			return &d.Devices[i], true
		}
	}
	return nil, false
}

// ShareByKeyHash returns the share a gateway key belongs to, for the gateway to
// resolve a member key to an account. A key with no live share is unknown, which
// the gateway turns into a 401 — instant revocation.
func (d *Data) ShareByKeyHash(keyHash string) (Share, bool) {
	for _, sh := range d.Shares {
		if sh.KeyHash == keyHash {
			return sh, true
		}
	}
	return Share{}, false
}

// HolderOf returns the assignment in force for an account, if any.
func (d *Data) HolderOf(accountID string, now time.Time) (Assignment, bool) {
	for _, a := range d.Assignments {
		if a.AccountID == accountID && a.Active(now) {
			return a, true
		}
	}
	return Assignment{}, false
}

// Holdings returns everything a person has in force, newest first.
func (d *Data) Holdings(personID string, now time.Time) []Assignment {
	var out []Assignment
	for _, a := range d.Assignments {
		if a.PersonID == personID && a.Active(now) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GrantedAt.After(out[j].GrantedAt) })
	return out
}

// log appends one line of activity.
func (d *Data) log(at time.Time, who, what string) {
	d.Activity = append(d.Activity, Event{At: at, Who: who, What: what})
}

// Log appends one line of activity. Callers outside this package use it so the
// log is written in the same transaction as the change it describes.
func (d *Data) Log(at time.Time, who, what string) { d.log(at, who, what) }

// ---------------------------------------------------------------- decisions

// Assign lends an account to a person until a time (zero: until it is taken
// back).
//
// An account works on one machine at a time, so an account that is already out
// has to come back first. That happens here, in the same locked section, and it
// is recorded as its own end — "given to someone else" — rather than being
// silently overwritten, because the person who lost it deserves the log to say
// why.
func (s *Store) Assign(accountID, personID string, until time.Time, moveIfBusy bool) (Assignment, error) {
	var made Assignment
	err := s.Mutate(func(d *Data) error {
		now := s.now()
		acct, ok := d.Account(accountID)
		if !ok {
			return fmt.Errorf("no account with id %q", accountID)
		}
		if _, ok := d.Person(personID); !ok {
			return fmt.Errorf("no person with id %q", personID)
		}
		if held, busy := d.HolderOf(accountID, now); busy {
			if held.PersonID == personID {
				return fmt.Errorf("%s already has %s", d.personName(personID), acct.Name)
			}
			if !moveIfBusy {
				return ErrAccountBusy
			}
			d.end(&held, now, EndedReassigned)
			d.replace(held)
			d.log(now, "You", fmt.Sprintf("took %s back from %s to give it to %s",
				acct.Name, d.personName(held.PersonID), d.personName(personID)))
		}
		made = Assignment{
			ID:        newID(),
			AccountID: accountID,
			PersonID:  personID,
			GrantedAt: now,
			ExpiresAt: until,
		}
		d.Assignments = append(d.Assignments, made)
		d.log(now, "You", fmt.Sprintf("gave %s to %s%s", acct.Name, d.personName(personID), forHowLong(now, until)))
		return nil
	})
	return made, err
}

// TakeBack ends an assignment. why is one of the Ended* reasons.
func (s *Store) TakeBack(assignmentID, why, who string) error {
	return s.Mutate(func(d *Data) error {
		now := s.now()
		for i := range d.Assignments {
			a := &d.Assignments[i]
			if a.ID != assignmentID {
				continue
			}
			if !a.Active(now) {
				return nil // already over; taking it back again is not an error
			}
			d.end(a, now, why)
			verb := "took"
			if why == EndedHandedBack {
				verb = "handed"
			}
			d.log(now, who, fmt.Sprintf("%s %s back from %s", verb,
				d.accountName(a.AccountID), d.personName(a.PersonID)))
			return nil
		}
		return fmt.Errorf("no assignment with id %q", assignmentID)
	})
}

// IssueShare makes an account available to a person through the gateway and
// returns the gateway key to hand out (stored only as a hash). Issuing it again
// for the same pair replaces the key, so a lost key is rotated by re-issuing.
// IssueShare makes an account available to a person through the gateway and
// returns the gateway key. sealKey seals it for later delivery to the person's
// device (the caller supplies it because only it holds the panel key).
func (s *Store) IssueShare(accountID, personID string, sealKey func(string) []byte) (key string, err error) {
	key, hash, err := NewToken()
	if err != nil {
		return "", err
	}
	sealed := sealKey(key)
	err = s.Mutate(func(d *Data) error {
		if _, ok := d.Account(accountID); !ok {
			return fmt.Errorf("no account with id %q", accountID)
		}
		if _, ok := d.Person(personID); !ok {
			return fmt.Errorf("no person with id %q", personID)
		}
		out := d.Shares[:0]
		for _, sh := range d.Shares {
			if !(sh.AccountID == accountID && sh.PersonID == personID) {
				out = append(out, sh)
			}
		}
		d.Shares = append(out, Share{ID: newID(), AccountID: accountID, PersonID: personID, KeyHash: hash, SealedKey: sealed, CreatedAt: s.now()})
		d.Log(s.now(), "You", fmt.Sprintf("gave %s access to %s", d.personName(personID), d.accountName(accountID)))
		return nil
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// RevokeShare withdraws a person's gateway access to an account; their key stops
// working at the gateway on the next request.
func (s *Store) RevokeShare(shareID string) error {
	return s.Mutate(func(d *Data) error {
		out := d.Shares[:0]
		var removed *Share
		for i := range d.Shares {
			if d.Shares[i].ID == shareID {
				removed = &d.Shares[i]
				continue
			}
			out = append(out, d.Shares[i])
		}
		if removed == nil {
			return nil
		}
		d.Shares = out
		d.Log(s.now(), "You", fmt.Sprintf("took %s's access to %s away", d.personName(removed.PersonID), d.accountName(removed.AccountID)))
		return nil
	})
}

func (d *Data) end(a *Assignment, now time.Time, why string) {
	a.EndedAt = now
	a.EndedWhy = why
}

// replace writes a copy back over the stored assignment of the same id.
func (d *Data) replace(a Assignment) {
	for i := range d.Assignments {
		if d.Assignments[i].ID == a.ID {
			d.Assignments[i] = a
			return
		}
	}
}

// forHowLong says how long an assignment is for, the way a person would.
//
// It rounds rather than truncating. The deadline is worked out when the request
// arrives and the clock is read again when it is written, so a day is a hair
// under twenty-four hours by the time it gets here — and truncating turned
// every "a day" into "23 hours".
func forHowLong(now, until time.Time) string {
	if until.IsZero() {
		return ", until it is taken back"
	}
	hours := int(math.Round(until.Sub(now).Hours()))
	switch {
	case hours >= 48:
		return fmt.Sprintf(" for %d days", int(math.Round(float64(hours)/24)))
	case hours >= 24:
		return " for a day"
	case hours >= 2:
		return fmt.Sprintf(" for %d hours", hours)
	default:
		return fmt.Sprintf(" for %d minutes", int(math.Round(until.Sub(now).Minutes())))
	}
}

// newID is a short random identifier. These are never guessed at by anyone —
// they are handed out by the panel — so eight bytes is plenty.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice, and a time-based fallback
		// would be a silently weaker id. Fail loudly instead.
		panic("panel: no randomness available: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// jsonMarshal is exposed to tests that need to build a raw backend blob.
func jsonMarshal(d Data) ([]byte, error) { return json.MarshalIndent(d, "", "  ") }
