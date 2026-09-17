package panel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/config"
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
}

// Change is what one check-in altered, for the caller to report.
type Change struct {
	Gained []string
	Lost   []string
}

// Empty reports whether the check-in changed nothing, which is the usual case.
func (c Change) Empty() bool { return len(c.Gained) == 0 && len(c.Lost) == 0 }

// CheckIn asks the panel what this machine is entitled to and makes that true.
//
// The panel's answer is a complete list, so anything held that is not in it is
// given back. That is the whole mechanism: nothing has to reach the machine to
// take an account away, because the machine asks and then acts on the answer.
//
// This writes a credentials file, which Part 1 of this work otherwise took ccam
// out of the business of doing. The distinction is real and worth keeping
// straight: what was removed was ccam *copying* logins between stores it did
// not own, which is what destroyed two accounts. This writes a login an
// administrator deliberately handed to this machine, into a directory ccam made
// for it — the same thing `claude auth login` would put there. It never touches
// an account the user made themselves, and it never touches a keychain.
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
		Assignments []clientAssignment `json:"assignments"`
		Error       string             `json:"error"`
	}
	err := post(ctx, httpc, c.Config.Server+"/api/v1/checkin", c.Config.Token, struct{}{}, &out)
	if errors.Is(err, errUnauthorized) {
		// Cut off: give everything back, then say so.
		change, _ = c.releaseAll()
		return change, ErrNotEnrolled
	}
	if err != nil {
		return change, err
	}
	if out.Error != "" {
		return change, errors.New(out.Error)
	}

	local, err := c.Accounts.List()
	if err != nil {
		return change, err
	}
	held := map[string]accounts.Account{}
	for _, a := range local {
		if a.PanelID != "" {
			held[a.PanelID] = a
		}
	}

	for _, want := range out.Assignments {
		acct, ok := held[want.AccountID]
		if !ok {
			made, err := c.Accounts.Add(want.Name)
			if err != nil {
				return change, fmt.Errorf("taking delivery of %s: %w", want.Name, err)
			}
			if acct, err = c.Accounts.SetPanelID(made.ID, want.AccountID); err != nil {
				return change, err
			}
			// A previous revocation of this same account may have left a marker;
			// clear it so the fresh grant's session is not stopped on sight.
			_ = config.ClearRevoked(made.ID)
			change.Gained = append(change.Gained, want.Name)
		}
		delete(held, want.AccountID)

		if want.Credential == "" {
			continue // the panel is not holding a login for this one yet
		}
		raw, err := base64.StdEncoding.DecodeString(want.Credential)
		if err != nil {
			return change, fmt.Errorf("the login sent for %s could not be read: %w", want.Name, err)
		}
		if err := writeCredential(acct.ConfigDir, raw); err != nil {
			return change, err
		}
		if acct.Status != accounts.StatusLinked {
			if _, err := c.Accounts.SetStatus(acct.ID, accounts.StatusLinked); err != nil {
				return change, err
			}
		}
	}

	// Anything still here is something the panel did not mention.
	for _, stale := range held {
		if err := c.release(stale); err != nil {
			return change, err
		}
		change.Lost = append(change.Lost, stale.Name)
	}

	if !change.Empty() && c.AfterChange != nil {
		c.AfterChange()
	}
	return change, nil
}

// releaseAll gives back everything the panel ever lent this machine.
func (c *Client) releaseAll() (Change, error) {
	var change Change
	local, err := c.Accounts.List()
	if err != nil {
		return change, err
	}
	for _, a := range local {
		if a.PanelID == "" {
			continue
		}
		if err := c.release(a); err != nil {
			return change, err
		}
		change.Lost = append(change.Lost, a.Name)
	}
	if !change.Empty() && c.AfterChange != nil {
		c.AfterChange()
	}
	return change, nil
}

// release gives one account back: the login goes first, so that a failure
// halfway leaves a machine that cannot use the account rather than one that
// still can.
func (c *Client) release(a accounts.Account) error {
	if a.PanelID == "" {
		return fmt.Errorf("refusing to give back %s: it is not the panel's to take", a.Name)
	}
	// Delete the login wherever it lives — the file, and on macOS the Keychain
	// item Claude Code migrates it into on first refresh. Deleting only the file
	// would leave the real credential behind after the member had used it once.
	if a.ConfigDir != "" {
		accounts.RemoveLogin(a.ConfigDir)
	}
	// Tell any live session on this account to stop. The supervisor polls for
	// this marker, so a running session ends within a poll tick rather than
	// limping on its in-memory token. Best-effort.
	_ = config.MarkRevoked(a.ID)
	_, err := c.Accounts.Remove(a.ID)
	return err
}

// writeCredential puts a login where Claude Code will look for it, via a temp
// file and a rename so a crash cannot leave a half-written one — the failure
// mode that started all of this.
func writeCredential(configDir string, raw []byte) error {
	if strings.TrimSpace(configDir) == "" {
		return errors.New("refusing to write a login without an account directory to put it in")
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(configDir, ".credentials-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(configDir, ".credentials.json"))
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
