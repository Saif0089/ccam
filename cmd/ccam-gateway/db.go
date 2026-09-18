package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"ccam/internal/gateway"
	"ccam/panel"
	"ccam/panelpg"
)

// dbUpstream serves the gateway from the same database the admin panel manages.
// A member's key maps (by hash) to a share, a share to an account, and an
// account to a live, self-refreshing token. Many members' keys can point at one
// account — that is how one login serves the whole team at once.
type dbUpstream struct {
	store  *panel.Store
	secret *panel.Secret

	mu       sync.Mutex
	data     panel.Data
	dataAt   time.Time
	managers map[string]*gateway.Manager // accountID -> token manager
}

func newDBUpstream(ctx context.Context, dsn, keyB64 string) (*dbUpstream, error) {
	secret, err := panel.SecretFromBase64(keyB64)
	if err != nil {
		return nil, err
	}
	back, err := panelpg.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &dbUpstream{
		store:    panel.NewStoreWithBackend(back),
		secret:   secret,
		managers: map[string]*gateway.Manager{},
	}, nil
}

// dataTTL is how long the loaded panel state is reused before re-reading, so a
// new share or a removed one takes effect within a few seconds without a DB hit
// per request.
const dataTTL = 15 * time.Second

func (u *dbUpstream) snapshot() panel.Data {
	u.mu.Lock()
	defer u.mu.Unlock()
	if time.Since(u.dataAt) < dataTTL {
		return u.data
	}
	return u.reloadLocked()
}

// reloadLocked re-reads the panel now, ignoring the TTL. u.mu must be held.
func (u *dbUpstream) reloadLocked() panel.Data {
	if d, err := u.store.Load(); err == nil {
		u.data = d
		u.dataAt = time.Now()
	}
	return u.data
}

// Resolve maps a member key to the account's current access token.
//
// A key that is not in the cached snapshot triggers one fresh read before it is
// declared unknown, so a share created moments ago works on the first request
// rather than after the cache's TTL — the window that produced a spurious
// "access has been withdrawn" right after granting access.
func (u *dbUpstream) Resolve(memberKey string) (accessToken, label string, err error) {
	keyHash := panel.HashToken(memberKey)
	d := u.snapshot()
	share, found := d.ShareByKeyHash(keyHash)
	if !found {
		u.mu.Lock()
		d = u.reloadLocked()
		u.mu.Unlock()
		if share, found = d.ShareByKeyHash(keyHash); !found {
			return "", "", gateway.ErrUnknownKey
		}
	}
	acct, ok := d.Account(share.AccountID)
	if !ok {
		return "", "", gateway.ErrUnknownKey // the account was removed
	}
	if !acct.HasLogin() {
		return "", "", fmt.Errorf("%s has no stored login", acct.Name)
	}
	mgr, err := u.managerFor(*acct)
	if err != nil {
		return "", "", fmt.Errorf("opening the shared login for %s: %w", acct.Name, err)
	}
	token, err := mgr.Token(context.Background())
	if err != nil {
		// The login can't be rolled forward — its refresh token is dead, which
		// happens when the same account is still being used first-party
		// somewhere. Drop the cached manager so a re-added login is picked up.
		u.forget(acct.ID)
		return "", "", fmt.Errorf("refreshing the shared login for %s: %w", acct.Name, err)
	}
	return token, acct.Name, nil
}

// forget drops an account's cached token manager, so the next request rebuilds
// it from whatever the database now holds (e.g. a login the admin re-added).
func (u *dbUpstream) forget(accountID string) {
	u.mu.Lock()
	delete(u.managers, accountID)
	u.mu.Unlock()
}

// managerFor returns the one refreshing token-manager for an account, building
// it from the sealed credential the panel stored. All members of an account
// share the same manager, so the token is refreshed once, centrally.
func (u *dbUpstream) managerFor(acct panel.Account) (*gateway.Manager, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if m, ok := u.managers[acct.ID]; ok {
		return m, nil
	}
	raw, err := u.secret.Open(acct.Credential)
	if err != nil {
		return nil, err
	}
	access, refreshTok, expires := parseCredential(raw)
	accountID := acct.ID
	m := gateway.NewManager(access, refreshTok, expires, func(fresh gateway.Credential) {
		u.persist(accountID, fresh)
	})
	u.managers[acct.ID] = m
	return m, nil
}

// persist seals a rotated credential back into the account, so the single-use
// refresh token that just replaced the old one is not lost on restart.
func (u *dbUpstream) persist(accountID string, fresh gateway.Credential) {
	_ = u.store.Mutate(func(d *panel.Data) error {
		acct, ok := d.Account(accountID)
		if !ok {
			return nil
		}
		raw, err := u.secret.Open(acct.Credential)
		if err != nil {
			return nil
		}
		updated := updateCredential(raw, fresh)
		sealed, err := u.secret.Seal(updated)
		if err != nil {
			return nil
		}
		acct.Credential = sealed
		return nil
	})
}

// parseCredential pulls the tokens out of a stored claudeAiOauth blob.
func parseCredential(raw []byte) (access, refreshTok string, expires time.Time) {
	var b struct {
		O struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return "", "", time.Time{}
	}
	if b.O.ExpiresAt > 0 {
		expires = time.UnixMilli(b.O.ExpiresAt)
	}
	return b.O.AccessToken, b.O.RefreshToken, expires
}

// updateCredential writes fresh tokens back into a stored blob, preserving every
// other field (scopes, subscriptionType, ...).
func updateCredential(raw []byte, fresh gateway.Credential) []byte {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		m = map[string]any{}
	}
	oauth, _ := m["claudeAiOauth"].(map[string]any)
	if oauth == nil {
		oauth = map[string]any{}
	}
	oauth["accessToken"] = fresh.AccessToken
	if fresh.RefreshToken != "" {
		oauth["refreshToken"] = fresh.RefreshToken
	}
	oauth["expiresAt"] = fresh.ExpiresAt.UnixMilli()
	m["claudeAiOauth"] = oauth
	out, _ := json.Marshal(m)
	return out
}
