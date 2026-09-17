package panel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	Activity    []Event      `json:"activity,omitempty"`
}

// Store is the panel's persistence. Every mutation takes the lock, re-reads
// from disk, decides, and writes atomically, so a crash mid-write leaves the
// previous file intact rather than a half-written one.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

// NewStore returns a Store backed by path, which need not exist yet.
func NewStore(path string) *Store {
	return &Store{path: path, now: time.Now}
}

// Load reads the panel. A missing file is an empty panel, not an error: that is
// what a machine looks like before anyone has set it up.
func (s *Store) Load() (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (Data, error) {
	var d Data
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return d, err
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("reading %s: %w", s.path, err)
	}
	return d, nil
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

	d, err := s.loadLocked()
	if err != nil {
		return err
	}
	s.sealExpired(&d)
	if err := fn(&d); err != nil {
		return err
	}
	return s.saveLocked(&d)
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

func (s *Store) saveLocked(d *Data) error {
	if len(d.Activity) > maxActivity {
		d.Activity = d.Activity[len(d.Activity)-maxActivity:]
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".panel-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
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
