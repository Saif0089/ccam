package panel

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"ccam/internal/accounts"
	"ccam/internal/config"
)

// setupLending gets a harness to the point where "Work" is escrowed and Alice
// has a machine enrolled, and returns the machine's config plus Alice's id.
func setupLending(t *testing.T, h *harness) (cfg ClientConfig, accountID, personID string) {
	t.Helper()
	h.do("POST", "/api/setup", map[string]string{"password": "a-long-enough-one"}, "")
	h.do("POST", "/api/accounts", map[string]string{"name": "Work"}, "")
	h.do("POST", "/api/people", map[string]string{"name": "Alice"}, "")

	_, panelBody := h.do("GET", "/api/panel", nil, "")
	accountID = panelBody["accounts"].([]any)[0].(map[string]any)["id"].(string)
	personID = panelBody["people"].([]any)[0].(map[string]any)["id"].(string)

	login := base64.StdEncoding.EncodeToString([]byte(`{"claudeAiOauth":{"accessToken":"lent"}}`))
	h.do("POST", "/api/accounts/"+accountID+"/login", map[string]string{"credential": login}, "")

	_, codeBody := h.do("POST", "/api/people/"+personID+"/code", nil, "")
	_, enrolled := h.do("POST", "/api/v1/enroll",
		map[string]any{"code": codeBody["code"], "machine": "alice-mbp"}, "")
	return ClientConfig{
		Server:   h.srv.URL,
		DeviceID: enrolled["deviceId"].(string),
		Token:    enrolled["token"].(string),
	}, accountID, personID
}

func newTestManager(t *testing.T) *accounts.Manager {
	t.Helper()
	dir := t.TempDir()
	return accounts.NewManager(
		accounts.NewStore(filepath.Join(dir, "accounts.json")),
		filepath.Join(dir, "accounts"))
}

// Taking an account back has to reach the machine, and reaching it has to mean
// the login is actually gone from the disk — not just a status somewhere.
func TestCheckInTakesDeliveryAndThenGivesItBack(t *testing.T) {
	// release() drops a revocation marker under ~/.ccam; keep it out of the
	// real home and let us assert it.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	h := newHarness(t)
	cfg, accountID, personID := setupLending(t, h)

	mgr := newTestManager(t)
	// An account this machine's owner made themselves. The panel has no
	// business touching it, whatever happens.
	mine, err := mgr.Add("Personal")
	if err != nil {
		t.Fatal(err)
	}

	c := &Client{Config: cfg, Accounts: mgr}

	// Nothing assigned yet.
	if change, err := c.CheckIn(context.Background()); err != nil || !change.Empty() {
		t.Fatalf("first check-in: %+v %v", change, err)
	}

	h.do("POST", "/api/assign", map[string]any{"accountId": accountID, "personId": personID}, "")

	change, err := c.CheckIn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Gained) != 1 || change.Gained[0] != "Work" {
		t.Fatalf("gained %v, want [Work]", change.Gained)
	}

	list, err := mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	var lent accounts.Account
	for _, a := range list {
		if a.PanelID == accountID {
			lent = a
		}
	}
	if lent.ID == "" {
		t.Fatal("the lent account was not created locally")
	}
	credPath := filepath.Join(lent.ConfigDir, ".credentials.json")
	raw, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("the login was not written where Claude Code looks: %v", err)
	}
	if string(raw) != `{"claudeAiOauth":{"accessToken":"lent"}}` {
		t.Errorf("written login = %s", raw)
	}
	// File permissions are a Unix concept; Windows reports 0666 for every file.
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(credPath); err == nil && info.Mode().Perm() != 0o600 {
			t.Errorf("the login is readable by others: mode %v", info.Mode().Perm())
		}
	}

	// Now take it back.
	_, panelBody := h.do("GET", "/api/panel", nil, "")
	assignmentID := panelBody["accounts"].([]any)[0].(map[string]any)["assignmentId"].(string)
	h.do("POST", "/api/assignments/"+assignmentID+"/takeback", nil, "")

	change, err = c.CheckIn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Lost) != 1 || change.Lost[0] != "Work" {
		t.Fatalf("lost %v, want [Work]", change.Lost)
	}
	if _, err := os.Stat(credPath); !os.IsNotExist(err) {
		t.Error("the login is still on disk after the account was taken back")
	}
	// A live session on this account would be stopped by this marker.
	if !config.IsRevoked(lent.ID) {
		t.Error("take-back did not leave a revocation marker for a live session to see")
	}
	list, _ = mgr.List()
	for _, a := range list {
		if a.PanelID == accountID {
			t.Error("the lent account is still registered after being taken back")
		}
	}

	// The account the user made is untouched throughout.
	if _, err := mgr.Get(mine.ID); err != nil {
		t.Errorf("the panel removed an account it was never lent: %v", err)
	}
}

// A machine that has been cut off gives everything back, without needing to be
// told account by account.
func TestACutOffMachineGivesEverythingBack(t *testing.T) {
	h := newHarness(t)
	cfg, accountID, personID := setupLending(t, h)
	mgr := newTestManager(t)
	c := &Client{Config: cfg, Accounts: mgr}

	h.do("POST", "/api/assign", map[string]any{"accountId": accountID, "personId": personID}, "")
	if _, err := c.CheckIn(context.Background()); err != nil {
		t.Fatal(err)
	}

	if code, _ := h.do("DELETE", "/api/devices/"+cfg.DeviceID, nil, ""); code != 204 {
		t.Fatal("cutting the machine off failed")
	}

	change, err := c.CheckIn(context.Background())
	if err != ErrNotEnrolled {
		t.Fatalf("check-in after being cut off = %v, want ErrNotEnrolled", err)
	}
	if len(change.Lost) != 1 {
		t.Errorf("gave back %v, want the one account it held", change.Lost)
	}
	list, _ := mgr.List()
	for _, a := range list {
		if a.PanelID != "" {
			t.Error("a cut-off machine is still holding a lent account")
		}
	}
}

// ccam without a panel is ccam as it always was.
func TestAMachineWithNoPanelDoesNothing(t *testing.T) {
	mgr := newTestManager(t)
	if _, err := mgr.Add("Personal"); err != nil {
		t.Fatal(err)
	}
	c := &Client{Accounts: mgr}
	change, err := c.CheckIn(context.Background())
	if err != nil || !change.Empty() {
		t.Fatalf("check-in with no panel configured = %+v %v, want a no-op", change, err)
	}
	if list, _ := mgr.List(); len(list) != 1 {
		t.Error("an unconfigured check-in changed the account list")
	}
}
