package accounts

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPruneRemovesTranscriptsButKeepsWhatWasNeverCopied(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, work, "-repo", "s.jsonl", "big transcript body")
	// The migration copies projects/ and nothing else, so everything below is
	// this account's only copy. Deleting any of it loses it for good.
	mustFile(t, filepath.Join(work, "skills", "x"), "skill")
	mustFile(t, filepath.Join(work, "history.jsonl"), "typed prompts")
	mustFile(t, filepath.Join(work, "CLAUDE.md"), "instructions")
	mustFile(t, filepath.Join(work, "shell-snapshots", "snap.sh"), "cache")
	mustFile(t, filepath.Join(work, ".claude.json"), `{"oauthAccount":{"accountUuid":"w"}}`)
	mustFile(t, filepath.Join(work, ".credentials.json"), `{"secret":true}`)
	// Already shared, so projects/ is safe to remove.
	mustFile(t, filepath.Join(claudeDir, "projects", "-repo", "s.jsonl"), "big transcript body")

	mgr, _ := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationCredentialsOnly},
	})

	// Dry run removes nothing.
	dry, err := mgr.Prune(claudeDir, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.TotalBytes() == 0 {
		t.Errorf("dry run should report reclaimable bytes, got %+v", dry.Accounts)
	}
	if _, err := os.Stat(filepath.Join(work, "projects")); err != nil {
		t.Error("dry run must not delete anything")
	}

	rep, err := mgr.Prune(claudeDir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Accounts) != 1 {
		t.Fatalf("want 1 account pruned, got %d", len(rep.Accounts))
	}
	for _, gone := range []string{"projects", "shell-snapshots"} {
		if _, err := os.Stat(filepath.Join(work, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should have been pruned", gone)
		}
	}
	for _, keep := range []string{".claude.json", ".credentials.json", "skills", "history.jsonl", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(work, keep)); err != nil {
			t.Errorf("%s must be kept — nothing ever copied it anywhere else: %v", keep, err)
		}
	}
}

// A shell opened before the migration keeps the old alias and goes on writing
// into the account's own projects/. Those transcripts are nowhere else, so
// prune has to copy them across before it may delete them.
func TestPruneSharesTranscriptsThatOnlyExistInTheAccountDir(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, work, "-repo", "after-the-flip.jsonl", "a whole day of work")

	mgr, _ := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationCredentialsOnly},
	})
	rep, err := mgr.Prune(claudeDir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Accounts[0].Err != nil {
		t.Fatalf("prune reported an error: %v", rep.Accounts[0].Err)
	}
	if rep.Accounts[0].Shared != 1 {
		t.Errorf("Shared = %d, want 1 transcript copied before deletion", rep.Accounts[0].Shared)
	}
	got, err := os.ReadFile(filepath.Join(claudeDir, "projects", "-repo", "after-the-flip.jsonl"))
	if err != nil || string(got) != "a whole day of work" {
		t.Errorf("transcript did not survive prune: %q, %v", got, err)
	}
}

// A transcript copied while its session was still writing left a shorter file
// in the shared tree. The longer local one must replace it, not be discarded.
func TestPruneReplacesAShortSharedCopy(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, work, "-repo", "s.jsonl", "first half plus the second half")
	mustFile(t, filepath.Join(claudeDir, "projects", "-repo", "s.jsonl"), "first half")

	mgr, _ := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationCredentialsOnly},
	})
	if _, err := mgr.Prune(claudeDir, nil, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(claudeDir, "projects", "-repo", "s.jsonl"))
	if err != nil || string(got) != "first half plus the second half" {
		t.Errorf("truncated shared copy was not topped up: %q, %v", got, err)
	}
}

// A shared copy that is LONGER is a conversation someone resumed after the
// migration. The stale local one must never overwrite it.
func TestPruneNeverOverwritesANewerSharedCopy(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, work, "-repo", "s.jsonl", "the old, shorter copy")
	mustFile(t, filepath.Join(claudeDir, "projects", "-repo", "s.jsonl"), "the old, shorter copy plus everything since")

	mgr, _ := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationCredentialsOnly},
	})
	if _, err := mgr.Prune(claudeDir, nil, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(claudeDir, "projects", "-repo", "s.jsonl"))
	if err != nil || string(got) != "the old, shorter copy plus everything since" {
		t.Errorf("prune clobbered the resumed conversation: %q, %v", got, err)
	}
}

// If even one transcript cannot be copied across, the account is left entirely
// alone: prune never deletes a tree it could not duplicate.
func TestPruneDeletesNothingWhenATranscriptCannotBeShared(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block writes the same way on Windows")
	}
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".ccam", "accounts", "work")
	writeTranscript(t, work, "-repo", "only-here.jsonl", "irreplaceable")
	mustDir(t, filepath.Join(claudeDir, "projects"))
	if err := os.Chmod(filepath.Join(claudeDir, "projects"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(claudeDir, "projects"), 0o700) })

	mgr, _ := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationCredentialsOnly},
	})
	rep, err := mgr.Prune(claudeDir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Accounts[0].Err == nil {
		t.Error("prune should have reported that it could not share a transcript")
	}
	if _, err := os.Stat(filepath.Join(work, "projects", "-repo", "only-here.jsonl")); err != nil {
		t.Error("the only copy of a transcript was deleted after a failed share")
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

	rep, err := mgr.Prune(filepath.Join(home, ".claude"), nil, false)
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

	if _, err := mgr.Prune(filepath.Join(home, ".claude"), []string{"a"}, false); err != nil {
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

func mustDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

// A projects/ that is a symlink to a tree somewhere else (transcripts moved to
// another disk, say) walks as zero files: WalkDir will not descend into a
// symlinked root. That read as "nothing to migrate, all clean", flipped the
// account, and then let prune delete the link — leaving the real tree orphaned
// and in no shared copy. Both steps must refuse it instead.
func TestSymlinkedProjectsIsNeitherMigratedNorPruned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".ccam", "accounts", "work")
	elsewhere := filepath.Join(home, "elsewhere", "projects")
	mustFile(t, filepath.Join(elsewhere, "-repo", "s.jsonl"), "the real transcripts")
	mustDir(t, work)
	if err := os.Symlink(elsewhere, filepath.Join(work, "projects")); err != nil {
		t.Fatal(err)
	}

	mgr, store := seedStore(t, []Account{
		{ID: "work", Kind: KindManaged, ConfigDir: work, Isolation: IsolationConfigDir},
	})

	rep, err := mgr.MigrateManagedToShared(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Migrated() {
		t.Error("an account whose projects/ is a symlink must not be flipped as migrated")
	}
	list, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].IsolationOrDefault() != IsolationConfigDir {
		t.Errorf("isolation = %q, want it left on the old scheme", list[0].IsolationOrDefault())
	}

	// And if it somehow is credentials-only already, prune still refuses.
	if _, err := store.Mutate(func(cur []Account) ([]Account, error) {
		cur[0].Isolation = IsolationCredentialsOnly
		return cur, nil
	}); err != nil {
		t.Fatal(err)
	}
	pr, err := mgr.Prune(claudeDir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Accounts[0].Err == nil {
		t.Error("prune should have refused a symlinked projects tree")
	}
	if _, err := os.Lstat(filepath.Join(work, "projects")); err != nil {
		t.Error("prune deleted the symlink")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "-repo", "s.jsonl")); err != nil {
		t.Error("the linked transcripts are gone")
	}
}
