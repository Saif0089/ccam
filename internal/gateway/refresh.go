package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// The gateway is the single place a shared subscription's token is refreshed.
// No client ever rotates it, so two live sessions on one login stop colliding —
// that collision is the whole reason the one-holder model existed, and it is
// what this removes.

var testTokenEndpoint string // set only by tests

const (
	tokenEndpoint = "https://platform.claude.com/v1/oauth/token"
	// claudeCodeClientID is Claude Code's public OAuth client id (from the
	// shipped binary). It is not a secret — it is visible in the login URL.
	claudeCodeClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	// refreshLead is how long before expiry the token is refreshed, so a request
	// is never served with one about to die mid-stream.
	refreshLead = 10 * time.Minute
)

// Credential is the part of a Claude login the gateway forwards with and rolls
// forward. The refresh token is single-use: each refresh returns a new one, and
// the old one dies — which is exactly why only the gateway may hold it.
type Credential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

func (c Credential) stale(now time.Time) bool {
	return c.AccessToken == "" || !now.Before(c.ExpiresAt.Add(-refreshLead))
}

// refresh trades a refresh token for a fresh credential.
func refresh(ctx context.Context, httpc *http.Client, refreshToken string) (Credential, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     claudeCodeClientID,
	})
	ep := tokenEndpoint
	if testTokenEndpoint != "" {
		ep = testTokenEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("refreshing the shared login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Credential{}, fmt.Errorf("refreshing the shared login: the token service answered %s", resp.Status)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Credential{}, err
	}
	if out.AccessToken == "" {
		return Credential{}, fmt.Errorf("the token service returned no access token")
	}
	return Credential{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

// Refresh trades a refresh token for a fresh credential against the live token
// endpoint. It is exported for the gateway's `diagnose` command; ordinary
// serving goes through a Manager, which caches and persists. Note the refresh
// token is single-use — a successful call rotates it, so the caller must
// persist the result or the old token is lost.
func Refresh(ctx context.Context, refreshToken string) (Credential, error) {
	return refresh(ctx, &http.Client{Timeout: 30 * time.Second}, refreshToken)
}

// Manager keeps one account's credential fresh and hands out usable access
// tokens. It is the exported handle the DB-backed gateway builds one of per
// shared account.
type Manager = tokenManager

// NewManager builds a token manager for one account's credential. onRefresh
// persists a rotated credential (in practice, sealed back into the database).
func NewManager(access, refreshTok string, expiresAt time.Time, onRefresh func(Credential)) *Manager {
	return newTokenManager(Credential{AccessToken: access, RefreshToken: refreshTok, ExpiresAt: expiresAt}, onRefresh)
}

// Token returns a currently-valid access token, refreshing first if needed.
func (m *Manager) Token(ctx context.Context) (string, error) { return m.get(ctx) }

// tokenManager keeps one account's credential fresh. Get returns a usable access
// token, refreshing first if it is close to expiry, and persists a refreshed
// credential through onRefresh so a restart does not lose the rotation.
type tokenManager struct {
	mu        sync.Mutex
	cred      Credential
	httpc     *http.Client
	now       func() time.Time
	onRefresh func(Credential) // persist the rotated credential (e.g. back to the DB)
}

func newTokenManager(cred Credential, onRefresh func(Credential)) *tokenManager {
	return &tokenManager{cred: cred, httpc: &http.Client{Timeout: 30 * time.Second}, now: time.Now, onRefresh: onRefresh}
}

func (m *tokenManager) get(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cred.stale(m.now()) {
		return m.cred.AccessToken, nil
	}
	fresh, err := refresh(ctx, m.httpc, m.cred.RefreshToken)
	if err != nil {
		// Fall back to the current token if it has not literally expired yet —
		// a refresh blip should not take the account down.
		if m.cred.AccessToken != "" && m.now().Before(m.cred.ExpiresAt) {
			return m.cred.AccessToken, nil
		}
		return "", err
	}
	m.cred = fresh
	if m.onRefresh != nil {
		m.onRefresh(fresh)
	}
	return fresh.AccessToken, nil
}
