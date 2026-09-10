package cli

import (
	"path/filepath"
	"testing"

	"ccam/internal/accounts"
	"ccam/internal/credstore"
)

const (
	loginA = `{"claudeAiOauth":{"accessToken":"account-a-token","expiresAt":1}}`
	loginB = `{"claudeAiOauth":{"accessToken":"account-b-token","expiresAt":2}}`
)

// The session runs on a copy, so switching it cannot disturb anyone else.
func TestSessionCredsSeedsAPrivateCopy(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acct := accounts.Account{ID: "a", Slug: "a", ConfigDir: filepath.Join(base, "a")}
	if err := credstore.Write(acct.ConfigDir, []byte(loginA)); err != nil {
		t.Fatal(err)
	}
	creds := newSessionCreds(filepath.Join(base, "sessions"), acct, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	t.Cleanup(creds.close)
	if creds.dir() == acct.ConfigDir {
		t.Fatal("the session is reading the account's own store, not a copy")
	}
	if !credstore.Same(acct.ConfigDir, creds.dir()) {
		t.Error("the copy does not hold what the account holds")
	}
}

// THE rule. An account's store is read-only to ccam: it is copied out of and
// never written back into, so no bug anywhere in switching can put one
// account's login where another account's belongs.
//
// This is the regression that matters. The old "mirror" copied the session's
// store back to whichever account the session was bookkept against, and a
// switch that wrote the store and then failed its read-back left the new
// account's login sitting there under the old account's name. On this machine
// three stores ended up byte-for-byte identical, so switching between them
// changed nothing and looked like a broken UI.
func TestSwitchingNeverWritesAnAccountsOwnStore(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	a := accounts.Account{ID: "a", Slug: "a", Name: "a", ConfigDir: filepath.Join(base, "a")}
	b := accounts.Account{ID: "b", Slug: "b", Name: "b", ConfigDir: filepath.Join(base, "b")}
	if err := credstore.Write(a.ConfigDir, []byte(loginA)); err != nil {
		t.Fatal(err)
	}
	if err := credstore.Write(b.ConfigDir, []byte(loginB)); err != nil {
		t.Fatal(err)
	}

	creds := newSessionCreds(filepath.Join(base, "sessions"), a, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	if ok, reason := creds.switchTo(b, "sess-1"); !ok {
		t.Fatalf("switch refused: %s", reason)
	}
	if creds.current().ID != "b" {
		t.Errorf("session is on %q, want b", creds.current().ID)
	}

	// Whatever Claude Code does inside the session — refresh a token, be
	// switched again — must never reach either account.
	if err := credstore.Write(creds.dir(), []byte(`{"claudeAiOauth":{"accessToken":"refreshed-inside-the-session"}}`)); err != nil {
		t.Fatal(err)
	}
	creds.close()

	for _, acct := range []accounts.Account{a, b} {
		want := map[string]string{"a": loginA, "b": loginB}[acct.ID]
		got, err := credstore.Read(acct.ConfigDir)
		if err != nil {
			t.Fatalf("account %s is unreadable after a session used it: %v", acct.ID, err)
		}
		if string(got) != want {
			t.Errorf("account %s holds %s\n            want %s", acct.ID, got, want)
		}
	}
}

// close takes the store away with it: a stale copy of a login must not be left
// lying around after the session ends.
func TestSessionCredsCloseRemovesTheStore(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acct := accounts.Account{ID: "a", ConfigDir: filepath.Join(base, "a")}
	if err := credstore.Write(acct.ConfigDir, []byte(loginA)); err != nil {
		t.Fatal(err)
	}
	creds := newSessionCreds(filepath.Join(base, "sessions"), acct, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	dir := creds.dir()
	creds.close()
	if _, err := credstore.Read(dir); err == nil {
		t.Error("the session's credentials are still readable after the session ended")
	}
}

// A session store that could not be seeded with the account's own credentials
// must not be used at all.
func TestSessionCredsRefusesAStoreThatDidNotTakeTheSeed(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acct := accounts.Account{ID: "a", ConfigDir: filepath.Join(base, "a")}
	if creds := newSessionCreds(filepath.Join(base, "sessions"), acct, ""); creds != nil {
		creds.close()
		t.Fatal("seeded a session store from an account that has no login")
	}
}
