package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// orgScopedCacheKeys are the keys in ~/.claude.json that Claude Code fills
// with data scoped to the logged-in account's organization. When the active
// account changes, they describe the WRONG org — a stale plan, model list, or
// usage banner — so they are cleared on a switch. Claude Code repopulates each
// from the new account on demand, so clearing them costs nothing but a first
// fetch.
var orgScopedCacheKeys = []string{
	"clientDataCacheSlots",
	"modelAccessCache",
	"cachedUsageUtilization",
	"orgModelDefaultCache",
	"additionalModelOptionsCache",
	"cachedExtraUsageDisabledReason",
}

// SnapshotDefaultIdentity captures the default account's identity so it can be
// restored when the user switches back to it.
//
// A managed account keeps its oauthAccount in its own <configDir>/.claude.json,
// but the default account's identity lives in the shared ~/.claude.json — the
// very field a switch to another account overwrites. So ccam snapshots it into
// a stub of its own (stubDir/.claude.json, stubDir being ~/.ccam/accounts/default)
// the first time, while ~/.claude.json still cleanly names the default account.
// That first time is safe by construction: before this feature every managed
// account was isolated, so nothing had ever rewritten ~/.claude.json.
//
// It runs once — if the stub already exists it does nothing, so it never
// captures an identity some later switch left behind. A shared file with no
// oauthAccount (not signed in) is left uncaptured, to try again next boot.
func SnapshotDefaultIdentity(claudeJSONPath, stubDir string) error {
	stub := filepath.Join(stubDir, ".claude.json")
	if _, err := os.Stat(stub); err == nil {
		return nil // already captured
	}
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	oa, ok := cfg["oauthAccount"].(map[string]any)
	if !ok || len(oa) == 0 {
		return nil // not signed in yet — capture on a later boot
	}
	if err := os.MkdirAll(stubDir, 0o700); err != nil {
		return err
	}
	out, err := json.MarshalIndent(map[string]any{"oauthAccount": oa}, "", "  ")
	if err != nil {
		return err
	}
	tmp := stub + ".ccam-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, stub)
}

// ReadOAuthAccount returns the oauthAccount object from an account's own
// identity stub (<configDir>/.claude.json), or nil if the file or the field
// is absent. This is the identity ccam keeps writing there on login, so it is
// always the correct account for that credential namespace.
func ReadOAuthAccount(configDir string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if oa, ok := cfg["oauthAccount"].(map[string]any); ok {
		return oa, nil
	}
	return nil, nil
}

// SetActiveIdentity makes the shared ~/.claude.json name the given account.
//
// With every credentials-only account sharing one ~/.claude.json, its single
// oauthAccount field would otherwise keep naming whichever account logged in
// last, so /status, the statusline and org-scoped requests would show the
// wrong identity for the session actually running. On a switch ccam writes the
// new account's oauthAccount here and clears the org-scoped caches. Everything
// else in the file — the user's projects, mcpServers, skillUsage — is
// preserved untouched.
//
// claudeJSONPath is the shared config file (~/.claude.json, the one Claude
// Code reads when CLAUDE_CONFIG_DIR is unset). oauth is the account's
// oauthAccount object, normally from ReadOAuthAccount.
//
// It is a no-op when oauth is nil (nothing to assert) or when the file already
// names this account (so relaunching the same account does not churn the file
// or drop caches needlessly). A missing or unparseable shared file is returned
// as an error for the caller to decide on — ccam must never clobber the user's
// real config with a fresh one.
func SetActiveIdentity(claudeJSONPath string, oauth map[string]any) error {
	if oauth == nil {
		return nil
	}
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	want, err := json.Marshal(oauth)
	if err != nil {
		return err
	}
	if have, err := json.Marshal(cfg["oauthAccount"]); err == nil && string(have) == string(want) {
		return nil // already the active identity
	}

	cfg["oauthAccount"] = oauth
	for _, k := range orgScopedCacheKeys {
		delete(cfg, k)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := claudeJSONPath + ".ccam-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, claudeJSONPath)
}
