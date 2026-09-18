package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccam/internal/accounts"
)

// ErrNotEnrolled means the panel no longer recognises this machine — it was
// cut off, or the panel was rebuilt. Everything the panel lent is given back.
var ErrNotEnrolled = errors.New("this machine is no longer enrolled with the panel")

// ClientConfig is what a machine remembers about the panel it answers to.
type ClientConfig struct {
	Server     string `json:"server"`
	DeviceID   string `json:"deviceId"`
	Token      string `json:"token"`
	PersonName string `json:"personName,omitempty"`
}

// Configured reports whether this machine answers to a panel at all. ccam
// without one behaves exactly as it always has.
func (c ClientConfig) Configured() bool {
	return strings.TrimSpace(c.Server) != "" && strings.TrimSpace(c.Token) != ""
}

// LoadClientConfig reads the panel a machine is enrolled with. A missing file
// means it is enrolled with none.
func LoadClientConfig(path string) (ClientConfig, error) {
	var c ClientConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

// SaveClientConfig writes it back, readable only by this user: it holds this
// machine's token.
func SaveClientConfig(path string, c ClientConfig) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// Enroll trades a one-shot join code for this machine's own token.
func Enroll(ctx context.Context, server, code, machine string) (ClientConfig, error) {
	var out struct {
		DeviceID   string `json:"deviceId"`
		Token      string `json:"token"`
		PersonName string `json:"personName"`
		Error      string `json:"error"`
	}
	if err := post(ctx, http.DefaultClient, server+"/api/v1/enroll", "",
		map[string]string{"code": code, "machine": machine}, &out); err != nil {
		return ClientConfig{}, err
	}
	if out.Error != "" {
		return ClientConfig{}, errors.New(out.Error)
	}
	return ClientConfig{Server: strings.TrimRight(server, "/"), DeviceID: out.DeviceID, Token: out.Token, PersonName: out.PersonName}, nil
}

// Client reconciles this machine against the panel.
type Client struct {
	Config   ClientConfig
	Accounts *accounts.Manager
	HTTP     *http.Client
	// AfterChange runs when something was gained or given back, so the caller
	// can re-sync shell aliases. Optional.
	AfterChange func()
	// SharesPath, when set, is where the gateway shares this machine was granted
	// are cached (0600), so `ccam shared <slug>` and the shell aliases can run a
	// shared account without the key ever touching a dotfile. Empty on a machine
	// that only ever runs its own accounts.
	SharesPath string
}

// GatewayShare is one shared account this machine may run through the gateway:
// where to route, and this person's key. It mirrors the panel's check-in reply.
type GatewayShare struct {
	Account string `json:"account"`
	Slug    string `json:"slug"`
	Gateway string `json:"gateway"`
	Key     string `json:"key"`
}

// LoadShares reads the cached gateway shares. A missing file is no shares.
func LoadShares(path string) ([]GatewayShare, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []GatewayShare
	return out, json.Unmarshal(raw, &out)
}

// SaveShares writes the cached gateway shares, readable only by this user: it
// holds gateway keys.
func SaveShares(path string, shares []GatewayShare) error {
	raw, err := json.MarshalIndent(shares, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// Change is what one check-in altered, for the caller to report: the accounts
// this machine can newly use, and the ones it can no longer use.
type Change struct {
	Gained []string
	Lost   []string
}

// Empty reports whether the check-in changed nothing, which is the usual case.
func (c Change) Empty() bool { return len(c.Gained) == 0 && len(c.Lost) == 0 }

// CheckIn asks the panel which accounts are shared with this machine and makes
// that true: it caches the gateway keys (so `ccam shared <slug>` and the shell
// aliases can run them) and re-syncs aliases when the set changed.
//
// Nothing here writes a Claude credential. That is the point of the gateway:
// the login stays on the server, the machine only ever holds a scoped key, and
// taking access away is the server dropping the share — the key simply stops
// working on the next request. This is what makes sharing safe and switching
// non-fragile, in place of the old model that copied logins onto disk.
func (c *Client) CheckIn(ctx context.Context) (Change, error) {
	var change Change
	if !c.Config.Configured() {
		return change, nil
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 20 * time.Second}
	}

	var out struct {
		Gateway []GatewayShare `json:"gateway"`
		Error   string         `json:"error"`
	}
	err := post(ctx, httpc, c.Config.Server+"/api/v1/checkin", c.Config.Token, struct{}{}, &out)
	if errors.Is(err, errUnauthorized) {
		// Cut off: forget every shared account, then say so.
		return c.forgetShares(), ErrNotEnrolled
	}
	if err != nil {
		return change, err
	}
	if out.Error != "" {
		return change, errors.New(out.Error)
	}

	change = c.applyShares(out.Gateway)
	if !change.Empty() && c.AfterChange != nil {
		c.AfterChange()
	}
	return change, nil
}

// applyShares caches the shares just fetched and reports what changed, by
// account name, against what was cached — so an unchanged check-in reports
// nothing and does not rewrite dotfiles.
func (c *Client) applyShares(next []GatewayShare) Change {
	var change Change
	if c.SharesPath == "" {
		return change
	}
	prev, _ := LoadShares(c.SharesPath)
	change = diffShares(prev, next)
	if !change.Empty() {
		_ = SaveShares(c.SharesPath, next)
	}
	return change
}

// forgetShares drops every cached share, as when this machine is cut off.
func (c *Client) forgetShares() Change {
	change := c.applyShares(nil)
	if !change.Empty() && c.AfterChange != nil {
		c.AfterChange()
	}
	return change
}

// diffShares reports which accounts are newly usable and which are gone, keyed
// by the stable slug and reported by name.
func diffShares(prev, next []GatewayShare) Change {
	was := map[string]string{}
	for _, s := range prev {
		was[s.Slug] = s.Account
	}
	now := map[string]string{}
	for _, s := range next {
		now[s.Slug] = s.Account
	}
	var change Change
	for slug, name := range now {
		if _, had := was[slug]; !had {
			change.Gained = append(change.Gained, name)
		}
	}
	for slug, name := range was {
		if _, still := now[slug]; !still {
			change.Lost = append(change.Lost, name)
		}
	}
	return change
}

var errUnauthorized = errors.New("unauthorized")

func post(ctx context.Context, httpc *http.Client, url, bearer string, body, out any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("the panel answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
