package accounts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneRemovesRedundantDirsButKeepsCredentialsAndIdentity(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".ccam", "accounts", "work")
	// A migrated (credentials-only) account: credential + identity to keep,
	// plus redundant trees that now live in the shared ~/.claude.
	writeTranscript(t, work, "-repo", "s.jsonl", "big transcript body")
	mustFile(t, filepath.Join(work, "skills", "x"), "skill")
	mustFile(t, filepath.Join(work, "history.jsonl"), "history")
	mustFile(t, filepath.Join(work, ".claude.json"), `{"oauthAccount":{"accountUuid":"w"}}`)
	mustFile(t, filepath.Join(work, ".credentials.json"), `{"secret":true}`)

	mgr, _ := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationCredentialsOnly},
	})

	// Dry run removes nothing.
	dry, err := mgr.Prune(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.TotalBytes() == 0 {
		t.Errorf("dry run should report reclaimable bytes, got %+v", dry.Accounts)
	}
	if _, err := os.Stat(filepath.Join(work, "projects")); err != nil {
		t.Error("dry run must not delete anything")
	}

	// Real prune.
	rep, err := mgr.Prune(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Accounts) != 1 {
		t.Fatalf("want 1 account pruned, got %d", len(rep.Accounts))
	}
	// Redundant trees gone.
	for _, gone := range []string{"projects", "skills", "history.jsonl"} {
		if _, err := os.Stat(filepath.Join(work, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should have been pruned", gone)
		}
	}
	// Credential + identity kept.
	for _, keep := range []string{".claude.json", ".credentials.json"} {
		if _, err := os.Stat(filepath.Join(work, keep)); err != nil {
			t.Errorf("%s must be kept: %v", keep, err)
		}
	}
}

func TestPruneSkipsUnmigratedAndDefaultAccounts(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".ccam", "accounts", "legacy")
	writeTranscript(t, legacy, "-repo", "s.jsonl", "x")

	mgr, _ := seedStore(t, []Account{
		{ID: "default", Kind: KindDefault, ConfigDir: "", Isolation: IsolationConfigDir},
		// still config-dir (not migrated) → must NOT be pruned, its transcripts
		// are its only copy.
		{ID: "legacy", Kind: KindManaged, ConfigDir: legacy, Isolation: IsolationConfigDir},
	})

	rep, err := mgr.Prune(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Accounts) != 0 {
		t.Errorf("no credentials-only accounts → nothing to prune, got %+v", rep.Accounts)
	}
	if _, err := os.Stat(filepath.Join(legacy, "projects")); err != nil {
		t.Error("an un-migrated account's transcripts must never be pruned")
	}
}

func TestPruneByID(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, ".ccam", "accounts", "a")
	b := filepath.Join(home, ".ccam", "accounts", "b")
	writeTranscript(t, a, "-r", "s.jsonl", "x")
	writeTranscript(t, b, "-r", "s.jsonl", "x")
	mgr, _ := seedStore(t, []Account{
		{ID: "a", Kind: KindManaged, ConfigDir: a, Isolation: IsolationCredentialsOnly},
		{ID: "b", Kind: KindManaged, ConfigDir: b, Isolation: IsolationCredentialsOnly},
	})

	if _, err := mgr.Prune([]string{"a"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a, "projects")); !os.IsNotExist(err) {
		t.Error("a should be pruned")
	}
	if _, err := os.Stat(filepath.Join(b, "projects")); err != nil {
		t.Error("b was not named, must be untouched")
	}
}

func mustFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
