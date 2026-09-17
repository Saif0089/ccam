package panel

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	s := NewStore(filepath.Join(t.TempDir(), "panel.json"))
	s.now = func() time.Time { return clock }
	return s, &clock
}

// seed puts one account and two people in, and returns their ids.
func seed(t *testing.T, s *Store) (account, alice, bob string) {
	t.Helper()
	account, alice, bob = newID(), newID(), newID()
	err := s.Mutate(func(d *Data) error {
		d.Accounts = append(d.Accounts, Account{ID: account, Name: "Work"})
		d.People = append(d.People,
			Person{ID: alice, Name: "Alice"},
			Person{ID: bob, Name: "Bob"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

// The rule the whole panel exists to enforce. Claude Code rotates its refresh
// token on every renewal, so two machines holding one login break each other —
// this is the same failure that once merged three accounts into one.
func TestAnAccountIsOnlyEverWithOnePerson(t *testing.T) {
	s, _ := newTestStore(t)
	account, alice, bob := seed(t, s)

	if _, err := s.Assign(account, alice, time.Time{}, false); err != nil {
		t.Fatal(err)
	}
	// Bob cannot have it while Alice does, unless the caller says to move it.
	if _, err := s.Assign(account, bob, time.Time{}, false); !errors.Is(err, ErrAccountBusy) {
		t.Fatalf("second assignment error = %v, want ErrAccountBusy", err)
	}

	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	held, ok := d.HolderOf(account, time.Now())
	if !ok || held.PersonID != alice {
		t.Fatal("Alice should still hold the account")
	}

	// Moving it ends Alice's hold in the same breath as starting Bob's.
	if _, err := s.Assign(account, bob, time.Time{}, true); err != nil {
		t.Fatal(err)
	}
	d, err = s.Load()
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, a := range d.Assignments {
		if a.AccountID == account && a.Active(time.Now()) {
			active++
			if a.PersonID != bob {
				t.Errorf("the account is with %s, want Bob", d.personName(a.PersonID))
			}
		}
	}
	if active != 1 {
		t.Fatalf("%d assignments in force for one account, want exactly 1", active)
	}
	// Alice's hold ended for a stated reason, rather than vanishing.
	for _, a := range d.Assignments {
		if a.PersonID == alice && a.EndedWhy != EndedReassigned {
			t.Errorf("Alice's assignment ended with %q, want %q", a.EndedWhy, EndedReassigned)
		}
	}
}

func TestTakingAnAccountBackTwiceIsNotAnError(t *testing.T) {
	s, _ := newTestStore(t)
	account, alice, _ := seed(t, s)
	made, err := s.Assign(account, alice, time.Time{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TakeBack(made.ID, EndedTakenBack, "You"); err != nil {
		t.Fatal(err)
	}
	// A second click, a retried request, a check-in that raced the first one.
	if err := s.TakeBack(made.ID, EndedTakenBack, "You"); err != nil {
		t.Errorf("taking it back again = %v, want no error", err)
	}
	d, _ := s.Load()
	if _, held := d.HolderOf(account, time.Now()); held {
		t.Error("the account is still out after being taken back")
	}
}

// An assignment that runs out while the panel is not running has to be over
// when it comes back, not over at the moment somebody first looks.
func TestAnAssignmentThatRanOutIsAlreadyOver(t *testing.T) {
	s, clock := newTestStore(t)
	account, alice, _ := seed(t, s)
	if _, err := s.Assign(account, alice, clock.Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}

	*clock = clock.Add(3 * time.Hour)
	// Any mutation settles expiry; this one changes nothing else.
	if err := s.Mutate(func(*Data) error { return nil }); err != nil {
		t.Fatal(err)
	}

	d, _ := s.Load()
	if _, held := d.HolderOf(account, *clock); held {
		t.Error("an assignment past its time is still in force")
	}
	var sealed bool
	for _, a := range d.Assignments {
		if a.EndedWhy == EndedExpired && !a.EndedAt.IsZero() {
			sealed = true
			if !a.EndedAt.Equal(a.ExpiresAt) {
				t.Errorf("ended at %v, want the moment it expired (%v)", a.EndedAt, a.ExpiresAt)
			}
		}
	}
	if !sealed {
		t.Error("the assignment was not recorded as expired")
	}
	if !loggedContaining(d, "ran out") {
		t.Error("expiry was not written to the activity log")
	}
}

// The log is what answers "who had this, and when". Every decision writes to it
// in the same transaction as the change itself, so a change that happened is a
// change that is recorded.
func TestEveryDecisionIsLogged(t *testing.T) {
	s, _ := newTestStore(t)
	account, alice, _ := seed(t, s)
	made, err := s.Assign(account, alice, time.Time{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TakeBack(made.ID, EndedTakenBack, "You"); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Load()
	if !loggedContaining(d, "gave Work to Alice") {
		t.Error("no log line for the account being given out")
	}
	if !loggedContaining(d, "took Work back from Alice") {
		t.Error("no log line for the account being taken back")
	}
}

func TestAMissingFileIsAnEmptyPanel(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nothing-here.json"))
	d, err := s.Load()
	if err != nil {
		t.Fatalf("loading a panel that was never set up = %v, want no error", err)
	}
	if len(d.Accounts) != 0 || d.Admin != nil {
		t.Error("a machine nobody has set up should read as an empty panel")
	}
}

func loggedContaining(d Data, want string) bool {
	for _, e := range d.Activity {
		if strings.Contains(e.What, want) {
			return true
		}
	}
	return false
}

// The deadline is worked out when the request arrives and the clock read again
// when it is written, so "a day" arrives here a hair under 24 hours. Truncating
// turned every one of them into "23 hours".
func TestHowLongReadsTheWayAPersonWouldSayIt(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		until time.Time
		want  string
	}{
		{time.Time{}, ", until it is taken back"},
		{now.Add(24*time.Hour - time.Millisecond), " for a day"},
		{now.Add(24 * time.Hour), " for a day"},
		{now.Add(7*24*time.Hour - time.Millisecond), " for 7 days"},
		{now.Add(8 * time.Hour), " for 8 hours"},
		{now.Add(30 * time.Minute), " for 30 minutes"},
	}
	for _, c := range cases {
		if got := forHowLong(now, c.until); got != c.want {
			t.Errorf("forHowLong(+%v) = %q, want %q", c.until.Sub(now), got, c.want)
		}
	}
}
