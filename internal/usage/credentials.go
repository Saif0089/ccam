// Package usage reads an account's Claude Code credentials and reports
// what its plan limits currently look like — the same numbers the
// `/usage` slash command shows, plus how long the login itself lasts.
package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// storedCredentials mirrors the on-disk / in-Keychain JSON.
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
// Where they live differs by platform, and on macOS the Keychain entry
// is keyed by config directory: the default account is under
// "Claude Code-credentials" and every other one under
// "Claude Code-credentials-<first 8 hex of sha256(configDir)>". That
// derivation is Claude Code's own, verified against a real install; if
// it ever changes this degrades to "usage unavailable" rather than
// breaking anything.
func ReadCredentials(configDir string) (Credentials, error) {
	// On macOS the Keychain is the usual home, but not the only one:
	// Claude Code writes the plain file when the Keychain is
	// unavailable, which is the normal case over SSH and in containers.
	// Try both, in that order, rather than assuming by platform.
	if runtime.GOOS == "darwin" {
		if raw, err := readKeychain(keychainService(configDir)); err == nil {
			return parseCredentials(raw)
		}
	}

	raw, err := os.ReadFile(CredentialsPath(configDir))
	if err != nil {
		// Both stores came up empty, which means nobody has signed this
		// account in — say that, rather than passing on an errno or a
		// `security` exit status that explains nothing to the reader.
		if os.IsNotExist(err) {
			return Credentials{}, errNoLogin
		}
		return Credentials{}, err
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

// keychainService is the macOS Keychain service name for an account.
func keychainService(configDir string) string {
	const base = "Claude Code-credentials"
	if configDir == "" {
		return base
	}
	sum := sha256.Sum256([]byte(configDir))
	return base + "-" + hex.EncodeToString(sum[:])[:8]
}

// readKeychain shells out to /usr/bin/security rather than calling the
// Keychain API directly. Keychain access is granted per executable, and
// the item was created by Claude Code — going through the same tool a
// person would use by hand means ccam inherits that tool's access
// instead of prompting under its own unfamiliar name.
func readKeychain(service string) ([]byte, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-w")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading this account's login from the Keychain: %w", err)
	}
	return out, nil
}

// CredentialsPath is where Claude Code keeps credentials on systems
// without a Keychain.
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
