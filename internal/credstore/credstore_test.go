package credstore

import (
	"path/filepath"
	"testing"
)

// Tests never touch the real keychain. Writing one from a test process — which
// has no login session and an overridden HOME — is what puts a "keychain could
// not be found, reset it?" dialog on the developer's screen. The file store is
// the same contract and the same code path everywhere else.
func fileStore(t *testing.T) { t.Helper(); t.Setenv(ForceFileEnvVar, "1") }

// The store is the thing an in-place account switch writes, so a round trip
// has to be exact: what comes back must be byte-for-byte what went in, or a
// session would read a corrupted login. Fabricated payloads only — this test
// never touches a real account's store, because the service name is derived
// from a directory that exists only for this test.
func TestRoundTripWriteReadDelete(t *testing.T) {
	fileStore(t)
	dir := filepath.Join(t.TempDir(), "session-store")
	payload := []byte(`{"claudeAiOauth":{"accessToken":"not-a-real-token","expiresAt":1}}`)

	if _, err := Read(dir); err == nil {
		t.Fatal("a fresh store should hold nothing")
	}
	if err := Write(dir, payload); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Delete(dir) })

	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("read back %q, want %q", got, payload)
	}

	// Overwriting is how a switch happens: the second write must win cleanly.
	other := []byte(`{"claudeAiOauth":{"accessToken":"a-different-fake","expiresAt":2}}`)
	if err := Write(dir, other); err != nil {
		t.Fatal(err)
	}
	got, err = Read(dir)
	if err != nil || string(got) != string(other) {
		t.Fatalf("after overwrite read %q (%v), want %q", got, err, other)
	}

	if err := Delete(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Error("store still readable after delete")
	}
	if err := Delete(dir); err != nil {
		t.Errorf("deleting an absent store should be fine, got %v", err)
	}
}

// Copy is both seeding a session store and switching it. It must leave the
// source alone and make the destination identical.
func TestCopyMakesTheDestinationIdenticalAndLeavesTheSource(t *testing.T) {
	fileStore(t)
	base := t.TempDir()
	from := filepath.Join(base, "account")
	to := filepath.Join(base, "session")
	payload := []byte(`{"claudeAiOauth":{"accessToken":"fake-source","expiresAt":3}}`)
	if err := Write(from, payload); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Delete(from); Delete(to) })

	if err := Copy(from, to); err != nil {
		t.Fatal(err)
	}
	if !Same(from, to) {
		t.Error("copy did not make the two stores identical")
	}
	src, err := Read(from)
	if err != nil || string(src) != string(payload) {
		t.Errorf("source changed: %q (%v)", src, err)
	}

	// And a switch: the destination becomes some other account, the source is
	// still untouched.
	third := filepath.Join(base, "other-account")
	if err := Write(third, []byte(`{"claudeAiOauth":{"accessToken":"fake-other","expiresAt":4}}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Delete(third) })
	if err := Copy(third, to); err != nil {
		t.Fatal(err)
	}
	if !Same(third, to) {
		t.Error("the switch did not land")
	}
	if Same(from, to) {
		t.Error("the session store still matches the account it was switched away from")
	}
}

func TestServiceNamesAreDistinctAndStable(t *testing.T) {
	fileStore(t)
	if Service("") != "Claude Code-credentials" {
		t.Errorf("the default login must use the bare item name, got %q", Service(""))
	}
	a, b := Service("/one"), Service("/two")
	if a == b {
		t.Error("two accounts must not share a keychain item")
	}
	if a != Service("/one") {
		t.Error("the same directory must always resolve to the same item")
	}
}
