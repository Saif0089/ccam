package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReadOAuthAccount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".claude.json"),
		[]byte(`{"oauthAccount":{"accountUuid":"abc","emailAddress":"e@x.com"},"other":1}`), 0o600)

	oa, err := ReadOAuthAccount(dir)
	if err != nil {
		t.Fatal(err)
	}
	if oa["accountUuid"] != "abc" {
		t.Errorf("accountUuid = %v, want abc", oa["accountUuid"])
	}

	// Missing file → nil, no error.
	if oa, err := ReadOAuthAccount(t.TempDir()); err != nil || oa != nil {
		t.Errorf("missing stub should be (nil,nil), got (%v,%v)", oa, err)
	}
}

func TestSetActiveIdentityWritesAccountAndClearsOrgCaches(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, ".claude.json")
	// A realistic shared file: another account's identity, org caches, and the
	// user's own data that must survive.
	os.WriteFile(shared, []byte(`{
      "oauthAccount": {"accountUuid":"OLD"},
      "modelAccessCache": {"stale":true},
      "cachedUsageUtilization": 42,
      "orgModelDefaultCache": {"x":1},
      "clientDataCacheSlots": [1,2],
      "additionalModelOptionsCache": {"y":2},
      "cachedExtraUsageDisabledReason": "nope",
      "projects": {"/repo": {"k":"v"}},
      "mcpServers": {"figma": {"url":"u"}}
    }`), 0o600)

	err := SetActiveIdentity(shared, map[string]any{"accountUuid": "NEW", "emailAddress": "n@x.com"})
	if err != nil {
		t.Fatal(err)
	}

	got := readJSON(t, shared)
	// Identity switched.
	if oa := got["oauthAccount"].(map[string]any); oa["accountUuid"] != "NEW" {
		t.Errorf("oauthAccount not updated: %v", oa)
	}
	// Every org-scoped cache cleared.
	for _, k := range orgScopedCacheKeys {
		if _, present := got[k]; present {
			t.Errorf("org-scoped cache %q should have been cleared", k)
		}
	}
	// The user's own data is preserved.
	if _, ok := got["projects"]; !ok {
		t.Error("projects must be preserved")
	}
	if _, ok := got["mcpServers"]; !ok {
		t.Error("mcpServers must be preserved")
	}
}

func TestSetActiveIdentityIsNoOpForSameAccount(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, ".claude.json")
	os.WriteFile(shared, []byte(`{"oauthAccount":{"accountUuid":"SAME"},"modelAccessCache":{"keep":true}}`), 0o600)

	before, _ := os.ReadFile(shared)
	if err := SetActiveIdentity(shared, map[string]any{"accountUuid": "SAME"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(shared)
	// Byte-identical: no rewrite, so the cache it would otherwise clear stays.
	if string(before) != string(after) {
		t.Errorf("same-account switch must not rewrite the file:\n before=%s\n after=%s", before, after)
	}
}

func TestSetActiveIdentityNeverCreatesAMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), ".claude.json")
	if err := SetActiveIdentity(missing, map[string]any{"accountUuid": "X"}); err == nil {
		t.Error("a missing shared config must return an error, not be created from scratch")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("ccam must not create ~/.claude.json itself")
	}
}

func TestSetActiveIdentityNilIsNoOp(t *testing.T) {
	if err := SetActiveIdentity(filepath.Join(t.TempDir(), "does-not-exist"), nil); err != nil {
		t.Errorf("nil oauth should be a silent no-op, got %v", err)
	}
}
