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
	if err := credstore.Write(acctDir, []byte(`{"claudeAiOauth":{"accessToken":"original"}}`)); err != nil {
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
	if err := credstore.Write(creds.dir(), []byte(`{"claudeAiOauth":{"accessToken":"refreshed"}}`)); err != nil {
		t.Fatal(err)
	}
	creds.mirror()

	got, err := credstore.Read(acctDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"claudeAiOauth":{"accessToken":"refreshed"}}` {
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
	if err := credstore.Write(from.ConfigDir, []byte(`{"claudeAiOauth":{"accessToken":"ehti-original"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := credstore.Write(to.ConfigDir, []byte(`{"claudeAiOauth":{"accessToken":"work"}}`)); err != nil {
		t.Fatal(err)
	}

	creds := newSessionCreds(filepath.Join(base, "sessions"), from, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	t.Cleanup(creds.close)

	if err := credstore.Write(creds.dir(), []byte(`{"claudeAiOauth":{"accessToken":"ehti-refreshed"}}`)); err != nil {
		t.Fatal(err)
	}
	if ok, reason := creds.switchTo(to, "sess-1"); !ok {
		t.Fatalf("switch was refused: %s", reason)
	}

	if got, _ := credstore.Read(from.ConfigDir); string(got) != `{"claudeAiOauth":{"accessToken":"ehti-refreshed"}}` {
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
	if err := credstore.Write(acct.ConfigDir, []byte(`{"claudeAiOauth":{"accessToken":"x"}}`)); err != nil {
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

// The mirror is the one write that can destroy an account: it copies whatever
// the session's store now holds over the account's own, which is the only copy
// of that login. On this machine a truncated keychain write left a fragment of
// the credentials in the session store, and the mirror faithfully copied it
// over both managed accounts — losing both logins for good. So a session store
// that no longer holds a login must not be copied back.
func TestSessionCredsRefusesToMirrorSomethingThatIsNotALogin(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acct := accounts.Account{ID: "ehti", Slug: "ehti", Name: "ehti", ConfigDir: filepath.Join(base, "ehti")}
	login := `{"claudeAiOauth":{"accessToken":"the-only-copy"}}`
	if err := credstore.Write(acct.ConfigDir, []byte(login)); err != nil {
		t.Fatal(err)
	}
	creds := newSessionCreds(filepath.Join(base, "sessions"), acct, "")
	if creds == nil {
		t.Fatal("could not seed a session store")
	}
	t.Cleanup(creds.close)

	// What a truncated write leaves behind: a fragment, not a login.
	if err := credstore.Write(creds.dir(), []byte(`07226d63704f41757468223a7b22`)); err != nil {
		t.Fatal(err)
	}
	creds.mirror()

	got, err := credstore.Read(acct.ConfigDir)
	if err != nil {
		t.Fatalf("the account store is unreadable after mirroring: %v", err)
	}
	if string(got) != login {
		t.Fatalf("the account login was overwritten with %q", got)
	}
}

// A session store that could not be seeded with the account's own credentials
// must not be used at all: the session would run on it and the mirror would
// eventually carry it back to the account.
func TestSessionCredsRefusesAStoreThatDidNotTakeTheSeed(t *testing.T) {
	t.Setenv(credstore.ForceFileEnvVar, "1")
	base := t.TempDir()
	acct := accounts.Account{ID: "a", ConfigDir: filepath.Join(base, "a")}
	// No credentials to seed from at all.
	if creds := newSessionCreds(filepath.Join(base, "sessions"), acct, ""); creds != nil {
		creds.close()
		t.Fatal("seeded a session store from an account that has no login")
	}
}
