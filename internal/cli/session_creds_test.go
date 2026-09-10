package cli

import (
	"path/filepath"
	"testing"

	"ccam/internal/accounts"
	"ccam/internal/credstore"
)

// A session refreshes its access token into whatever store it is reading, which
// is now a private one. Without mirroring, the account's own store would keep
// the token it had when the session started and go stale — and every new
// session of that account would start from the older credentials.
func TestSessionCredsMirrorsARefreshedTokenBackToTheAccount(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acctDir := filepath.Join(base, "account")
	acct := accounts.Account{ID: "work", Slug: "work", ConfigDir: acctDir}
	if err := credstore.Write(acctDir, []byte(`{"token":"original"}`)); err != nil {
		t.Fatal(err)
	}

	creds := newSessionCreds(filepath.Join(base, "sessions"), acct, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	t.Cleanup(creds.close)
	if !credstore.Same(acctDir, creds.dir()) {
		t.Fatal("session store was not seeded from the account")
	}

	// Claude Code refreshes the token in the session's own store.
	if err := credstore.Write(creds.dir(), []byte(`{"token":"refreshed"}`)); err != nil {
		t.Fatal(err)
	}
	creds.mirror()

	got, err := credstore.Read(acctDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"token":"refreshed"}` {
		t.Errorf("account store holds %s, want the refreshed token", got)
	}
}

// Switching mirrors first, so a token refreshed under the old account is not
// lost when the store is overwritten with the new one's.
func TestSessionCredsMirrorsBeforeSwitchingAway(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	from := accounts.Account{ID: "ehti", Slug: "ehti", ConfigDir: filepath.Join(base, "ehti")}
	to := accounts.Account{ID: "work", Slug: "work", ConfigDir: filepath.Join(base, "work")}
	if err := credstore.Write(from.ConfigDir, []byte(`{"token":"ehti-original"}`)); err != nil {
		t.Fatal(err)
	}
	if err := credstore.Write(to.ConfigDir, []byte(`{"token":"work"}`)); err != nil {
		t.Fatal(err)
	}

	creds := newSessionCreds(filepath.Join(base, "sessions"), from, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	t.Cleanup(creds.close)

	if err := credstore.Write(creds.dir(), []byte(`{"token":"ehti-refreshed"}`)); err != nil {
		t.Fatal(err)
	}
	if !creds.switchTo(to, "sess-1") {
		t.Fatal("switch was refused")
	}

	if got, _ := credstore.Read(from.ConfigDir); string(got) != `{"token":"ehti-refreshed"}` {
		t.Errorf("the account switched away from holds %s; its refreshed token was lost", got)
	}
	if !credstore.Same(to.ConfigDir, creds.dir()) {
		t.Error("the session is not on the account it switched to")
	}
	if creds.current().ID != "work" {
		t.Errorf("current account = %q, want work", creds.current().ID)
	}
}

// close takes the store away with it: a stale copy of a login must not be left
// lying around after the session ends.
func TestSessionCredsCloseRemovesTheStore(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acct := accounts.Account{ID: "a", ConfigDir: filepath.Join(base, "a")}
	if err := credstore.Write(acct.ConfigDir, []byte(`{"token":"x"}`)); err != nil {
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
