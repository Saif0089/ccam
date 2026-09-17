package panelpg

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"ccam/panel"
)

// dsn is the test database, or the test is skipped. CI without a database, and
// a developer without one, both skip cleanly; the integration is proven wherever
// CCAM_TEST_POSTGRES points at a real Postgres.
func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("CCAM_TEST_POSTGRES")
	if d == "" {
		t.Skip("set CCAM_TEST_POSTGRES to a Postgres DSN to run the database-backed tests")
	}
	return d
}

func freshBackend(t *testing.T) *Backend {
	t.Helper()
	b, err := Open(context.Background(), dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	// Start every test from an empty panel.
	if _, err := b.db.Exec(`UPDATE panel_state SET version = 0, data = '{}'::jsonb WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// The panel's own logic, unchanged, running over a real Postgres row: set up,
// add an account and a person, lend it out, and read it back.
func TestPanelRoundTripsThroughPostgres(t *testing.T) {
	b := freshBackend(t)
	store := panel.NewStoreWithBackend(b)

	account, alice := "", ""
	if err := store.Mutate(func(d *panel.Data) error {
		account = "acct-1"
		alice = "person-1"
		d.Accounts = append(d.Accounts, panel.Account{ID: account, Name: "Work"})
		d.People = append(d.People, panel.Person{ID: alice, Name: "Alice"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Assign(account, alice, timeZero(), false); err != nil {
		t.Fatal(err)
	}

	// A second Store over the same database — a different serverless instance —
	// sees the assignment.
	other := panel.NewStoreWithBackend(mustReopen(t))
	d, err := other.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, held := d.HolderOf(account, nowish()); !held {
		t.Fatal("a second instance does not see the assignment the first made")
	}
}

// The real thing this had to buy: two instances assigning the same free account
// at the same moment, and only one winning. Postgres settles the compare-and-swap.
func TestPostgresRefusesASecondHolderUnderRealConcurrency(t *testing.T) {
	b := freshBackend(t)
	setup := panel.NewStoreWithBackend(b)
	if err := setup.Mutate(func(d *panel.Data) error {
		d.Accounts = append(d.Accounts, panel.Account{ID: "acct", Name: "Work"})
		d.People = append(d.People,
			panel.Person{ID: "alice", Name: "Alice"},
			panel.Person{ID: "bob", Name: "Bob"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Two independent Stores, as two instances would be, racing to assign the
	// one free account to two different people.
	s1 := panel.NewStoreWithBackend(mustReopen(t))
	s2 := panel.NewStoreWithBackend(mustReopen(t))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	people := []string{"alice", "bob"}
	stores := []*panel.Store{s1, s2}
	wg.Add(2)
	for i := range stores {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = stores[i].Assign("acct", people[i], timeZero(), false)
		}(i)
	}
	wg.Wait()

	won := 0
	for _, e := range errs {
		if e == nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d of 2 racing assignments succeeded, want exactly 1", won)
	}

	d, _ := panel.NewStoreWithBackend(mustReopen(t)).Load()
	active := 0
	for _, a := range d.Assignments {
		if a.AccountID == "acct" && a.Active(nowish()) {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("%d active holders of one account after the race, want 1", active)
	}
}

var reopenDSN string

func mustReopen(t *testing.T) *Backend {
	t.Helper()
	if reopenDSN == "" {
		reopenDSN = dsn(t)
	}
	b, err := Open(context.Background(), reopenDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func timeZero() time.Time { return time.Time{} }
func nowish() time.Time   { return time.Now() }
