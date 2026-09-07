package usage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseCredentials(t *testing.T) {
	raw := []byte(`{"claudeAiOauth":{
		"accessToken":"sk-ant-oat01-xxx",
		"refreshToken":"sk-ant-ort01-yyy",
		"expiresAt":1757222399000,
		"refreshTokenExpiresAt":1759814399000,
		"scopes":["user:inference","user:profile"],
		"subscriptionType":"max",
		"rateLimitTier":"default_claude_max_20x"}}`)

	creds, err := parseCredentials(raw)
	if err != nil {
		t.Fatalf("parseCredentials: %v", err)
	}
	if creds.AccessToken != "sk-ant-oat01-xxx" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.SubscriptionType != "max" {
		t.Errorf("SubscriptionType = %q", creds.SubscriptionType)
	}
	if creds.RateLimitTier != "default_claude_max_20x" {
		t.Errorf("RateLimitTier = %q", creds.RateLimitTier)
	}
	if want := time.UnixMilli(1757222399000); !creds.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", creds.ExpiresAt, want)
	}
	if want := time.UnixMilli(1759814399000); !creds.RefreshExpiresAt.Equal(want) {
		t.Errorf("RefreshExpiresAt = %v, want %v", creds.RefreshExpiresAt, want)
	}
}

// A record with no token is a signed-out account, not a parse failure —
// and the page says so in different words, so keep them distinguishable.
func TestParseCredentialsWithoutToken(t *testing.T) {
	if _, err := parseCredentials([]byte(`{"claudeAiOauth":{}}`)); err == nil {
		t.Fatal("want an error when no token is stored")
	}
	if _, err := parseCredentials([]byte(`not json`)); err == nil {
		t.Fatal("want an error for malformed credentials")
	}
}

func TestParseCredentialsMissingExpiry(t *testing.T) {
	creds, err := parseCredentials([]byte(`{"claudeAiOauth":{"accessToken":"tok"}}`))
	if err != nil {
		t.Fatalf("parseCredentials: %v", err)
	}
	// Zero, not 1970: the UI hides the clock rather than claiming the
	// session ended 56 years ago.
	if !creds.ExpiresAt.IsZero() || !creds.RefreshExpiresAt.IsZero() {
		t.Errorf("want zero times, got %v / %v", creds.ExpiresAt, creds.RefreshExpiresAt)
	}
}

// The default account and a managed one are different Keychain items;
// mixing them up would show one account's usage under another's name.
func TestKeychainService(t *testing.T) {
	if got := keychainService(""); got != "Claude Code-credentials" {
		t.Errorf("default service = %q", got)
	}
	got := keychainService("/Users/x/.ccam/accounts/work")
	if len(got) != len("Claude Code-credentials")+9 {
		t.Errorf("service = %q, want an 8-hex suffix", got)
	}
	if got == keychainService("/Users/x/.ccam/accounts/home") {
		t.Error("two config dirs produced the same Keychain service")
	}
	if got != keychainService("/Users/x/.ccam/accounts/work") {
		t.Error("keychainService is not stable for the same config dir")
	}
}

func TestCredentialsPath(t *testing.T) {
	dir := t.TempDir()
	if got, want := CredentialsPath(dir), filepath.Join(dir, ".credentials.json"); got != want {
		t.Errorf("CredentialsPath = %q, want %q", got, want)
	}
	// The default account has no config dir of its own; it lives in
	// ~/.claude even though nothing sets CLAUDE_CONFIG_DIR for it.
	if got := CredentialsPath(""); filepath.Base(got) != ".credentials.json" {
		t.Errorf("default CredentialsPath = %q", got)
	}
}

func TestReadCredentialsFromFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"tok","subscriptionType":"pro"}}`)

	creds, err := ReadCredentials(dir)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	if creds.SubscriptionType != "pro" {
		t.Errorf("SubscriptionType = %q", creds.SubscriptionType)
	}
}
