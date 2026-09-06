package accounts

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "accounts.json"))
	return NewManager(store, filepath.Join(dir, "accounts"))
}

func TestAddCreatesAccountAndDir(t *testing.T) {
	m := newTestManager(t)

	a, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if a.Status != StatusPending {
		t.Errorf("status = %q, want pending", a.Status)
	}
	if a.Alias != "claude-work" {
		t.Errorf("alias = %q, want claude-work", a.Alias)
	}
	if info, err := os.Stat(a.ConfigDir); err != nil || !info.IsDir() {
		t.Errorf("config dir %q not created: %v", a.ConfigDir, err)
	}

	list, err := m.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %v, %v; want 1 account", list, err)
	}
}

func TestAddDeduplicatesSlugAndAlias(t *testing.T) {
	m := newTestManager(t)

	a1, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	a2, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	if a1.Slug == a2.Slug {
		t.Errorf("expected distinct slugs, got %q twice", a1.Slug)
	}
	if a1.Alias == a2.Alias {
		t.Errorf("expected distinct aliases, got %q twice", a1.Alias)
	}
	if a1.ConfigDir == a2.ConfigDir {
		t.Errorf("expected distinct config dirs, got %q twice", a1.ConfigDir)
	}
}

func TestRenameUpdatesAliasKeepsConfigDir(t *testing.T) {
	m := newTestManager(t)
	a, err := m.Add("Personal")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	originalDir := a.ConfigDir

	renamed, err := m.Rename(a.ID, "Side Project")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name != "Side Project" {
		t.Errorf("name = %q, want %q", renamed.Name, "Side Project")
	}
	if renamed.Alias != "claude-side-project" {
		t.Errorf("alias = %q, want claude-side-project", renamed.Alias)
	}
	if renamed.ConfigDir != originalDir {
		t.Errorf("config dir changed from %q to %q, want unchanged", originalDir, renamed.ConfigDir)
	}
	if renamed.ID != a.ID {
		t.Errorf("id changed from %q to %q, want unchanged", a.ID, renamed.ID)
	}
}

func TestRenameUnknownAccountErrors(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Rename("does-not-exist", "New Name"); err == nil {
		t.Fatal("expected error renaming unknown account, got nil")
	}
}

func TestSetStatusLinked(t *testing.T) {
	m := newTestManager(t)
	a, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	updated, err := m.SetStatus(a.ID, StatusLinked)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if updated.Status != StatusLinked {
		t.Errorf("status = %q, want linked", updated.Status)
	}
	if updated.LastUsedAt.IsZero() {
		t.Error("expected LastUsedAt to be set once linked")
	}
}

func TestRemoveDeletesAccountAndDir(t *testing.T) {
	m := newTestManager(t)
	a, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	removed, err := m.Remove(a.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed.ID != a.ID {
		t.Errorf("removed id = %q, want %q", removed.ID, a.ID)
	}
	if _, err := os.Stat(a.ConfigDir); !os.IsNotExist(err) {
		t.Errorf("expected config dir removed, stat err = %v", err)
	}

	list, err := m.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("List() after remove = %v, %v; want empty", list, err)
	}
}

func TestRemoveUnknownAccountErrors(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Remove("does-not-exist"); err == nil {
		t.Fatal("expected error removing unknown account, got nil")
	}
}

func TestGetUnknownAccountErrors(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Get("does-not-exist"); err == nil {
		t.Fatal("expected error getting unknown account, got nil")
	}
}
