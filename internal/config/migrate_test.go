package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The migration must carry a previous ~/.ccam install's metadata into ~/.clawdh
// verbatim, and must never move the account directories — an account's
// CLAUDE_CONFIG_DIR is stored absolutely and, on macOS, its Keychain login is
// keyed by that path, so moving it would sign the account out.
func TestMigrateFromLegacyPreservesMetadataAndLeavesAccountDirs(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".ccam")
	current := filepath.Join(root, ".clawdh")

	// A previous install: metadata files, plus an account directory whose
	// absolute path is what accounts.json references.
	mustWrite(t, filepath.Join(legacy, "accounts.json"),
		`{"accounts":[{"id":"abc","configDir":"`+legacy+`/accounts/abc"}]}`)
	mustWrite(t, filepath.Join(legacy, "shares.json"), `[{"slug":"work"}]`)
	mustWrite(t, filepath.Join(legacy, "panel-client.json"), `{"server":"x"}`)
	mustWrite(t, filepath.Join(legacy, "accounts", "abc", ".credentials.json"), `{"claudeAiOauth":{}}`)

	migrateFromLegacy(legacy, current)

	// Metadata is copied byte-for-byte (so the absolute configDir is preserved).
	for _, name := range []string{"accounts.json", "shares.json", "panel-client.json"} {
		got := mustRead(t, filepath.Join(current, name))
		want := mustRead(t, filepath.Join(legacy, name))
		if got != want {
			t.Errorf("%s: copied %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(mustRead(t, filepath.Join(current, "accounts.json")), legacy+"/accounts/abc") {
		t.Error("the migrated accounts.json must keep the account's original absolute configDir")
	}

	// The account directory is NOT moved: it stays in the legacy location, and
	// no accounts/ tree is created under the new one.
	if _, err := os.Stat(filepath.Join(legacy, "accounts", "abc", ".credentials.json")); err != nil {
		t.Error("the account directory must stay where it was (Keychain path)")
	}
	if _, err := os.Stat(filepath.Join(current, "accounts")); !os.IsNotExist(err) {
		t.Error("migration must not create an accounts/ directory under the new home")
	}
}

func TestMigrateIsIdempotentAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".ccam")
	current := filepath.Join(root, ".clawdh")
	mustWrite(t, filepath.Join(legacy, "accounts.json"), `{"from":"legacy"}`)
	// A file already present in the new home wins — the person may have used
	// clawdh since, and re-importing would clobber newer state.
	mustWrite(t, filepath.Join(current, "accounts.json"), `{"from":"clawdh"}`)

	migrateFromLegacy(legacy, current)
	migrateFromLegacy(legacy, current) // twice: must be a no-op the second time

	if got := mustRead(t, filepath.Join(current, "accounts.json")); got != `{"from":"clawdh"}` {
		t.Errorf("migration overwrote existing clawdh state: %q", got)
	}
}

func TestMigrateWithNoLegacyInstallDoesNothing(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, ".clawdh")
	migrateFromLegacy(filepath.Join(root, ".ccam"), current) // no ~/.ccam at all
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Error("a fresh install (no ~/.ccam) should not create the home dir here")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
