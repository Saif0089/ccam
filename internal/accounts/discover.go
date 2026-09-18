package accounts

import (
	"encoding/json"
	"os"
)

// DiscoveredLogin is a Claude login found on this machine, identified by the
// email it belongs to — so a person picks a real account instead of typing a
// name that has to match. ConfigDir is where its credential lives (empty for
// the default ~/.claude login), for capturing it later.
type DiscoveredLogin struct {
	Email     string `json:"email"`
	Plan      string `json:"plan"`
	ConfigDir string `json:"configDir"`
	IsDefault bool   `json:"isDefault"`
}

// DiscoverLogins finds every authenticated Claude login on this machine: the
// default ~/.claude one and each ccam-managed account, whether the credential
// lives in a file or the macOS Keychain. Each is paired with the email and plan
// Claude stored alongside it, and duplicates (the same account signed in more
// than one place) collapse to one entry.
func DiscoverLogins(list []Account) []DiscoveredLogin {
	var out []DiscoveredLogin
	seen := map[string]bool{}

	add := func(configDir string, isDefault bool) {
		raw, err := CaptureLogin(configDir)
		if err != nil {
			return // no login here
		}
		email, plan := identityFor(configDir, raw)
		key := email
		if key == "" {
			key = configDir // fall back so an unnamed login still shows once
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, DiscoveredLogin{Email: email, Plan: plan, ConfigDir: configDir, IsDefault: isDefault})
	}

	add("", true) // the default ~/.claude login
	for _, a := range list {
		if a.ConfigDir != "" {
			add(a.ConfigDir, false)
		}
	}
	return out
}

// identityFor reads the email (from the oauthAccount Claude writes) and the plan
// (from the captured credential). The email lives in ~/.claude.json for the
// default login and in <configDir>/.claude.json for a managed one.
func identityFor(configDir string, cred []byte) (email, plan string) {
	// The default login's identity lives in ~/.claude.json (in the home dir); a
	// managed account's lives in <configDir>/.claude.json.
	identityDir := configDir
	if identityDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			identityDir = home
		}
	}
	if oa, err := ReadOAuthAccount(identityDir); err == nil && oa != nil {
		if e, ok := oa["emailAddress"].(string); ok {
			email = e
		}
	}
	var c struct {
		O struct {
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(cred, &c) == nil {
		plan = c.O.SubscriptionType
	}
	return email, plan
}
