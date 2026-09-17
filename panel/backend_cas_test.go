package panel

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// racingBackend is a fake shared database: an in-memory blob with a version,
// and a hook that fires once, the first time a save is attempted, to simulate
// another instance winning the race in between this instance's read and write.
type racingBackend struct {
	mu      sync.Mutex
	raw     []byte
	version int64
	onFirst func()
	fired   bool
}

func (b *racingBackend) Load() ([]byte, int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.raw, b.version, nil
}

func (b *racingBackend) Save(raw []byte, expected int64) (bool, error) {
	if b.onFirst != nil && !b.fired {
		b.fired = true
		b.onFirst() // another writer lands here, moving the version
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.version != expected {
		return false, nil // lost the race; caller must re-read and retry
	}
	b.raw = append([]byte(nil), raw...)
	b.version++
	return true, nil
}

func (b *racingBackend) commit(raw []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.raw = append([]byte(nil), raw...)
	b.version++
}

// A serverless panel is several processes over one database. Two admins who
// assign the same free account at the same moment must not both succeed — the
// second write is refused, its decision re-runs against the first, and the
// invariant catches it. This drives that path deterministically.
func TestConcurrentAssignOfOneAccountLetsOnlyOneWin(t *testing.T) {
	back := &racingBackend{}
	s := NewStoreWithBackend(back)
	clock := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }

	account, alice, bob := seed(t, s)

	// The next Assign will, mid-save, discover that "the account was just given
	// to Bob" — exactly a concurrent instance winning first. The retry must see
	// Bob's hold and refuse Alice.
	back.onFirst = func() {
		// Build the winning state directly: Bob holds the account.
		d, _, _ := s.readLocked()
		d.Assignments = append(d.Assignments, Assignment{
			ID: newID(), AccountID: account, PersonID: bob, GrantedAt: clock,
		})
		raw := mustMarshal(t, d)
		back.commit(raw)
	}

	_, err := s.Assign(account, alice, time.Time{}, false)
	if err == nil {
		t.Fatal("Alice was assigned an account another instance had just given to Bob")
	}
	if !strings.Contains(err.Error(), "already has") && err != ErrAccountBusy {
		t.Fatalf("assign error = %v, want a refusal because Bob holds it", err)
	}

	d, _ := s.Load()
	holder, ok := d.HolderOf(account, clock)
	if !ok || holder.PersonID != bob {
		t.Fatal("after the race the account should be Bob's, and only Bob's")
	}
	active := 0
	for _, a := range d.Assignments {
		if a.AccountID == account && a.Active(clock) {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("%d active assignments for one account after a race, want 1", active)
	}
}

// A lost race that does NOT hit the invariant just retries and succeeds: adding
// two different people at once must not lose one.
func TestConcurrentUnrelatedWritesBothLand(t *testing.T) {
	back := &racingBackend{}
	s := NewStoreWithBackend(back)
	clock := time.Now()
	s.now = func() time.Time { return clock }

	// First add lands normally.
	if err := s.Mutate(func(d *Data) error {
		d.People = append(d.People, Person{ID: newID(), Name: "Alice"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Second add races once against a concurrent "Carol was added", then retries.
	back.onFirst = func() {
		d, _, _ := s.readLocked()
		d.People = append(d.People, Person{ID: newID(), Name: "Carol"})
		back.commit(mustMarshal(t, d))
	}
	if err := s.Mutate(func(d *Data) error {
		d.People = append(d.People, Person{ID: newID(), Name: "Bob"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	d, _ := s.Load()
	names := map[string]bool{}
	for _, p := range d.People {
		names[p.Name] = true
	}
	for _, want := range []string{"Alice", "Bob", "Carol"} {
		if !names[want] {
			t.Errorf("%s was lost to a race; have %v", want, names)
		}
	}
}

func mustMarshal(t *testing.T, d Data) []byte {
	t.Helper()
	raw, err := jsonMarshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
