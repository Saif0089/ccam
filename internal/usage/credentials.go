// Package usage reads an account's Claude Code credentials and reports
// what its plan limits currently look like — the same numbers the
// `/usage` slash command shows, plus how long the login itself lasts.
package usage

import (
	"clawdh/internal/accounts"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials is the part of Claude Code's stored OAuth record ccam
// needs. The tokens never leave this package's caller: they are used to
// call the API, and are never logged or sent to the browser.
type Credentials struct {
	AccessToken      string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
	SubscriptionType string
	RateLimitTier    string
	Scopes           []string
}

// storedCredentials mirrors the on-disk JSON.
type storedCredentials struct {
	ClaudeAIOAuth struct {
		AccessToken           string   `json:"accessToken"`
		RefreshToken          string   `json:"refreshToken"`
		ExpiresAt             int64    `json:"expiresAt"`
		RefreshTokenExpiresAt int64    `json:"refreshTokenExpiresAt"`
		SubscriptionType      string   `json:"subscriptionType"`
		RateLimitTier         string   `json:"rateLimitTier"`
		Scopes                []string `json:"scopes"`
	} `json:"claudeAiOauth"`
}

// ReadCredentials loads the credentials for one account.
//
// configDir is the account's CLAUDE_CONFIG_DIR, or empty for the
// default account — the one plain `claude` uses.
//
// This reads the credentials file and nothing else. ccam used to also
// derive and read Claude Code's macOS Keychain item, duplicating an
// undocumented name derivation that only Claude Code owns; the write
// half of that arrangement destroyed two real logins (see the 4 KB
// truncation in the history), so the whole of it is gone. On a machine
// where Claude Code keeps its credentials in the Keychain there is no
// file to read and usage reports itself unavailable, which is the
// honest answer rather than a guess.
func ReadCredentials(configDir string) (Credentials, error) {
	// Read the login wherever it lives — a file, or on macOS the Keychain.
	// Reading is safe (it is what shows the card its numbers); only writing a
	// credential store was ever dangerous, and ccam does not do that. Reusing
	// the account layer's capture keeps one derivation of the Keychain item
	// name, not two.
	raw, err := accounts.CaptureLogin(configDir)
	if err != nil {
		// Nothing readable anywhere means nobody has signed this account in.
		return Credentials{}, errNoLogin
	}
	return parseCredentials(raw)
}

// errNoLogin is the "this account has never been connected" case, which
// the page words differently from a login that has gone bad.
var errNoLogin = errors.New("no Claude login is stored for this account")

func parseCredentials(raw []byte) (Credentials, error) {
	var stored storedCredentials
	if err := json.Unmarshal(raw, &stored); err != nil {
		return Credentials{}, fmt.Errorf("parsing stored credentials: %w", err)
	}
	oauth := stored.ClaudeAIOAuth
	if oauth.AccessToken == "" {
		return Credentials{}, fmt.Errorf("no Claude account token stored for this account")
	}

	return Credentials{
		AccessToken:      oauth.AccessToken,
		ExpiresAt:        millisToTime(oauth.ExpiresAt),
		RefreshExpiresAt: millisToTime(oauth.RefreshTokenExpiresAt),
		SubscriptionType: oauth.SubscriptionType,
		RateLimitTier:    oauth.RateLimitTier,
		Scopes:           oauth.Scopes,
	}, nil
}

func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// CredentialsPath is where Claude Code keeps credentials when it is not
// using an OS keychain.
func CredentialsPath(configDir string) string {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".credentials.json"
		}
		return filepath.Join(home, ".claude", ".credentials.json")
	}
	return filepath.Join(configDir, ".credentials.json")
}
