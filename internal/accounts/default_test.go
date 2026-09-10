package accounts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// signInDefault makes the fake claude report the default account (the
// one with no CLAUDE_CONFIG_DIR) as signed in, by giving $HOME/.claude
// credentials.
func signInDefault(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Shaped like a real store: an account counts as linked only when its
	// credentials actually carry a login, so a placeholder no longer stands in.
	creds := []byte(`{"claudeAiOauth":{"accessToken":"not-a-real-token","expiresAt":1}}`)
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), creds, 0o600); err != nil {
		t.Fatalf("writing credentials: %v", err)
	}
}

func TestEnsureDefaultAdoptsTheSignedInAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	signInDefault(t, home)

	m := newTestManager(t)
	prober := &Prober{ClaudeBinary: buildFakeClaude(t)}

	added, err := m.EnsureDefault(context.Background(), prober)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if !added {
		t.Fatal("expected the default account to be adopted")
	}

	account, err := m.Get(DefaultAccountID)
	if err != nil {
		t.Fatalf("Get(default): %v", err)
	}
	if !account.IsDefault() {
		t.Errorf("kind = %q, want %q", account.Kind, KindDefault)
	}
	if account.Status != StatusLinked {
		t.Errorf("status = %q, want linked", account.Status)
	}
	if account.Alias != DefaultAccountAlias {
		t.Errorf("alias = %q, want %q", account.Alias, DefaultAccountAlias)
	}
	// The whole point: it is reached by *removing* the variable, so it
	// must not carry a config dir that something would then set.
	if account.ConfigDir != "" {
		t.Errorf("configDir = %q, want empty", account.ConfigDir)
	}
	if account.OwnsConfigDir() {
		t.Error("the default account must never own a directory ccam can delete")
	}

	// Running again must not duplicate it.
	if _, err := m.EnsureDefault(context.Background(), prober); err != nil {
		t.Fatalf("second EnsureDefault: %v", err)
	}
	list, _ := m.List()
	if len(list) != 1 {
		t.Errorf("accounts = %d, want 1", len(list))
	}
}

func TestEnsureDefaultIgnoresASignedOutDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// No credentials written: nothing to adopt.

	m := newTestManager(t)
	prober := &Prober{ClaudeBinary: buildFakeClaude(t)}

	added, err := m.EnsureDefault(context.Background(), prober)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if added {
		t.Error("adopted a default account that is not signed in")
	}
}

// TestRemovingTheDefaultForgetsItWithoutDeletingAnything is the safety
// property of this whole feature: that directory is the user's main
// Claude Code login.
func TestRemovingTheDefaultForgetsItWithoutDeletingAnything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	signInDefault(t, home)

	m := newTestManager(t)
	prober := &Prober{ClaudeBinary: buildFakeClaude(t)}
	if _, err := m.EnsureDefault(context.Background(), prober); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	if _, err := m.Remove(DefaultAccountID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	credentials := filepath.Join(home, ".claude", ".credentials.json")
	if _, err := os.Stat(credentials); err != nil {
		t.Fatalf("the user's real claude credentials were deleted: %v", err)
	}

	// And it must stay gone: re-adopting it would make removal pointless.
	added, err := m.EnsureDefault(context.Background(), prober)
	if err != nil {
		t.Fatalf("EnsureDefault after removal: %v", err)
	}
	if added {
		t.Error("the default account was re-adopted after the user removed it")
	}
	if list, _ := m.List(); len(list) != 0 {
		t.Errorf("accounts = %+v, want none", list)
	}
}

// TestAddRefusesAPopulatedDirectory guards the case where a previous
// removal could not delete the directory (routine on Windows, where an
// open handle blocks deletion): a new account must not silently inherit
// the old one's credentials.
func TestAddRefusesAPopulatedDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "accounts.json"))
	accountsDir := filepath.Join(dir, "accounts")
	m := NewManager(store, accountsDir)

	leftover := filepath.Join(accountsDir, "work")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leftover, ".credentials.json"), []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("seeding leftover credentials: %v", err)
	}

	account, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if account.ConfigDir == leftover {
		t.Fatalf("the new account adopted the leftover directory %s, inheriting its credentials", leftover)
	}
	if _, err := os.Stat(filepath.Join(account.ConfigDir, ".credentials.json")); err == nil {
		t.Error("the new account's directory already holds credentials")
	}
}
