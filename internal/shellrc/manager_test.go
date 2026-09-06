package shellrc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSyncerWritesAliasesToAllRcFiles(t *testing.T) {
	home := t.TempDir()
	s := NewSyncer(home)

	entries := []AliasEntry{
		{Alias: "claude-work", ConfigDir: filepath.Join(home, ".ccam/accounts/work")},
		{Alias: "claude-personal", ConfigDir: filepath.Join(home, ".ccam/accounts/personal")},
	}
	if err := s.Sync(entries); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for shell, path := range RcPaths(home) {
		has, err := HasBlock(path)
		if err != nil {
			t.Fatalf("HasBlock(%s): %v", path, err)
		}
		if !has {
			t.Errorf("%s: expected managed block in %s", shell, path)
		}
		data, _ := os.ReadFile(path)
		if !contains(string(data), "claude-work") || !contains(string(data), "claude-personal") {
			t.Errorf("%s: expected both aliases in %s, got %q", shell, path, data)
		}
	}
}

func TestSyncerWithNoAccountsRemovesBlock(t *testing.T) {
	home := t.TempDir()
	s := NewSyncer(home)

	entries := []AliasEntry{{Alias: "claude-work", ConfigDir: home}}
	if err := s.Sync(entries); err != nil {
		t.Fatalf("Sync (populate): %v", err)
	}
	if err := s.Sync(nil); err != nil {
		t.Fatalf("Sync (empty): %v", err)
	}

	for shell, path := range RcPaths(home) {
		has, err := HasBlock(path)
		if err != nil {
			t.Fatalf("HasBlock(%s): %v", path, err)
		}
		if has {
			t.Errorf("%s: expected no managed block once accounts list is empty", shell)
		}
	}
}

func TestSyncerRemoveAllLeavesUserContentIntact(t *testing.T) {
	home := t.TempDir()
	s := NewSyncer(home)

	// Seed one rc file with pre-existing user content, matching this
	// OS's actual rc path so RcPaths and this test agree.
	var seeded string
	for _, path := range RcPaths(home) {
		seeded = path
		break
	}
	original := "# my own aliases\nalias g='git'\n"
	if err := os.MkdirAll(filepath.Dir(seeded), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(seeded, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := s.Sync([]AliasEntry{{Alias: "claude-work", ConfigDir: home}}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := s.RemoveAll(); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	got, err := os.ReadFile(seeded)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Errorf("content = %q, want original %q", got, original)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRenderBodyEscapesForEachShell(t *testing.T) {
	entries := []AliasEntry{{Alias: "claude-work", ConfigDir: "/home/me/.ccam/accounts/work"}}

	cases := map[Shell]string{
		Bash:       `alias claude-work='CLAUDE_CONFIG_DIR="/home/me/.ccam/accounts/work" claude'`,
		Zsh:        `alias claude-work='CLAUDE_CONFIG_DIR="/home/me/.ccam/accounts/work" claude'`,
		Fish:       `alias claude-work 'env CLAUDE_CONFIG_DIR="/home/me/.ccam/accounts/work" claude'`,
		PowerShell: "function claude-work { $env:CLAUDE_CONFIG_DIR = '/home/me/.ccam/accounts/work'; claude @args }",
	}
	for shell, want := range cases {
		body := RenderBody(shell, entries)
		if !contains(body, want) {
			t.Errorf("%s: body %q does not contain %q", shell, body, want)
		}
	}
}

func TestRcPathsMatchesCurrentOS(t *testing.T) {
	paths := RcPaths(t.TempDir())
	if runtime.GOOS == "windows" {
		if _, ok := paths[PowerShell]; !ok || len(paths) != 1 {
			t.Errorf("windows RcPaths = %v, want only PowerShell", paths)
		}
	} else {
		for _, shell := range []Shell{Bash, Zsh, Fish, PowerShell} {
			if _, ok := paths[shell]; !ok {
				t.Errorf("missing %s in RcPaths on %s", shell, runtime.GOOS)
			}
		}
	}
}
