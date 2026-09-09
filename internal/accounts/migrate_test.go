package accounts

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// seedStore writes an accounts.json holding exactly the given accounts and
// returns a Manager over it.
func seedStore(t *testing.T, accounts []Account) (*Manager, *Store) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "accounts.json"))
	if _, err := store.Mutate(func([]Account) ([]Account, error) { return accounts, nil }); err != nil {
		t.Fatalf("seeding store: %v", err)
	}
	return NewManager(store, filepath.Join(dir, "accounts")), store
}

// writeTranscript creates <configDir>/projects/<slug>/<name> with content.
func writeTranscript(t *testing.T, configDir, slug, name, content string) {
	t.Helper()
	d := filepath.Join(configDir, "projects", slug)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func isolationOf(t *testing.T, store *Store, id string) Isolation {
	t.Helper()
	list, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.ID == id {
			return a.IsolationOrDefault()
		}
	}
	t.Fatalf("account %q not found", id)
	return ""
}

func TestMigrateCopiesTranscriptsAndFlipsIsolation(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	workDir := filepath.Join(home, ".ccam", "accounts", "work")

	// A managed account still on the old isolated scheme, with one transcript
	// and a nested subagent transcript, plus the two files that must NOT move.
	writeTranscript(t, workDir, "-repo", "sess-1.jsonl", `{"sessionId":"sess-1"}`)
	writeTranscript(t, workDir, "-repo/sess-1/subagents/wf", "agent-x.jsonl", `{"sessionId":"sess-1"}`)
	if err := os.WriteFile(filepath.Join(workDir, ".credentials.json"), []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"w"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr, store := seedStore(t, []Account{
		{ID: "default", Kind: KindDefault, ConfigDir: "", Isolation: IsolationConfigDir},
		{ID: "work", Kind: KindManaged, ConfigDir: workDir}, // isolation absent == config-dir
	})

	report, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !report.Migrated() {
		t.Fatal("report says nothing migrated, expected work to migrate")
	}

	// Transcripts (including the deep subagent one) are now in the shared tree.
	for _, rel := range []string{"-repo/sess-1.jsonl", "-repo/sess-1/subagents/wf/agent-x.jsonl"} {
		if _, err := os.Stat(filepath.Join(claudeDir, "projects", rel)); err != nil {
			t.Errorf("transcript %s not copied to shared ~/.claude: %v", rel, err)
		}
	}
	// The account flipped to credentials-only.
	if got := isolationOf(t, store, "work"); got != IsolationCredentialsOnly {
		t.Errorf("work isolation = %q, want credentials-only", got)
	}
	// The default account is untouched (still config-dir, no crash on empty configDir).
	if got := isolationOf(t, store, "default"); got != IsolationConfigDir {
		t.Errorf("default isolation = %q, want config-dir", got)
	}
	// Credentials and identity stub are left exactly where they were.
	for _, f := range []string{".credentials.json", ".claude.json"} {
		if _, err := os.Stat(filepath.Join(workDir, f)); err != nil {
			t.Errorf("%s must not be touched by migration: %v", f, err)
		}
	}
	// The source transcript is left in place (copy, not move — reversible).
	if _, err := os.Stat(filepath.Join(workDir, "projects", "-repo", "sess-1.jsonl")); err != nil {
		t.Errorf("source transcript should remain until an explicit prune: %v", err)
	}
	// No temp files left behind in the shared tree.
	_ = filepath.WalkDir(filepath.Join(claudeDir, "projects"), func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && strings.HasPrefix(filepath.Base(p), ".ccam-migrating") {
			t.Errorf("leftover temp file: %s", p)
		}
		return nil
	})
}

// TestCopyProjectsTreePropagatesRealStatError is the fix for the review's
// top correctness finding: a non-NotExist stat error must NOT be reported as
// "nothing to copy" (which would flip the account with its transcripts
// stranded). A regular file where a directory is expected makes os.Stat of
// "<file>/projects" return ENOTDIR, which is not os.IsNotExist.
func TestCopyProjectsTreePropagatesRealStatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX filesystem error semantics")
	}
	dir := t.TempDir()
	notADir := filepath.Join(dir, "notadir")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := copyProjectsTree(filepath.Join(notADir, "projects"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("a non-NotExist stat error must be propagated, got nil (account would be wrongly flipped)")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected a real (non-NotExist) error, got a NotExist: %v", err)
	}
}

// TestCopyProjectsTreeContinuesPastUnreadableEntry proves one bad entry does
// not abort the whole account: the good transcript is still copied and the
// bad one is counted as failed (so the caller declines to flip and retries).
func TestCopyProjectsTreeContinuesPastUnreadableEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX filesystem error semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	src, dst := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "good.jsonl"), []byte("g"), 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(src, "b")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })

	copied, _, failed, err := copyProjectsTree(src, dst)
	if err != nil {
		t.Fatalf("an unreadable subdir must not hard-fail the walk: %v", err)
	}
	if copied < 1 {
		t.Errorf("the good transcript should still be copied, copied=%d", copied)
	}
	if failed < 1 {
		t.Errorf("the unreadable subdir should be counted as failed, failed=%d", failed)
	}
}

// TestMigrateSkipsNonRegularFiles: a symlink in the tree is neither copied
// nor counted as a failure, so the account still flips.
func TestMigrateSkipsNonRegularFiles(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	workDir := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, workDir, "-repo", "real.jsonl", "R")
	if err := os.Symlink("/nonexistent", filepath.Join(workDir, "projects", "-repo", "link.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	mgr, _ := seedStore(t, []Account{{ID: "work", Kind: KindManaged, ConfigDir: workDir}})
	report, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Accounts[0].Copied != 1 || report.Accounts[0].Failed != 0 {
		t.Errorf("want 1 copied / 0 failed (symlink skipped), got %+v", report.Accounts[0])
	}
	if !report.Migrated() {
		t.Error("account should flip: a symlink is skipped, not a failure")
	}
	if _, err := os.Lstat(filepath.Join(claudeDir, "projects", "-repo", "link.jsonl")); err == nil {
		t.Error("symlink must not be copied into the shared tree")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	workDir := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, workDir, "-repo", "sess-1.jsonl", `{"sessionId":"sess-1"}`)

	mgr, _ := seedStore(t, []Account{{ID: "work", Kind: KindManaged, ConfigDir: workDir}})

	first, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Accounts) != 1 || first.Accounts[0].Copied != 1 {
		t.Fatalf("first run: want 1 copied, got %+v", first.Accounts)
	}

	// Second run: the account is now credentials-only, so it is skipped
	// entirely — nothing copied, nothing re-flipped.
	second, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if second.Migrated() || len(second.Accounts) != 0 {
		t.Fatalf("second run should be a no-op, got %+v", second.Accounts)
	}
}

func TestMigrateNeverOverwritesAnExistingTranscript(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	workDir := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, workDir, "-repo", "sess-1.jsonl", "SOURCE")

	// A file with the same name already exists in the shared tree (same
	// session UUID). It must be left exactly as-is.
	dst := filepath.Join(claudeDir, "projects", "-repo")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "sess-1.jsonl"), []byte("PRE-EXISTING"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr, _ := seedStore(t, []Account{{ID: "work", Kind: KindManaged, ConfigDir: workDir}})
	report, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Accounts[0].Copied != 0 || report.Accounts[0].Skipped != 1 {
		t.Errorf("want 0 copied / 1 skipped, got %+v", report.Accounts[0])
	}
	got, _ := os.ReadFile(filepath.Join(dst, "sess-1.jsonl"))
	if string(got) != "PRE-EXISTING" {
		t.Errorf("existing transcript was overwritten: %q", got)
	}
}

func TestMigrateAccountNeverUsedIsSafe(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	// A managed account whose directory exists but has no projects/ (created,
	// never used). Migration should flip it without error and copy nothing.
	workDir := filepath.Join(home, ".ccam", "accounts", "work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mgr, store := seedStore(t, []Account{{ID: "work", Kind: KindManaged, ConfigDir: workDir}})

	report, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if report.Accounts[0].Copied != 0 || !report.Accounts[0].Flipped {
		t.Errorf("empty account should flip with 0 copies, got %+v", report.Accounts[0])
	}
	if got := isolationOf(t, store, "work"); got != IsolationCredentialsOnly {
		t.Errorf("work isolation = %q, want credentials-only", got)
	}
}
