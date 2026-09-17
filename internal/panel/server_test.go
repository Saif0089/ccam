package panel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"path/filepath"
	"testing"
	"time"
)

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	store  *Store
	client *http.Client
	clock  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "panel.json"))
	secret, err := LoadSecret(filepath.Join(dir, "panel.key"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, store: store, clock: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	store.now = func() time.Time { return h.clock }

	ps := NewServer(store, secret)
	ps.now = func() time.Time { return h.clock }
	h.srv = httptest.NewServer(ps.Handler())
	t.Cleanup(h.srv.Close)

	jar := &cookieJar{}
	h.client = &http.Client{Jar: jar}
	return h
}

// do sends a request and returns status plus the decoded body.
func (h *harness) do(method, path string, body any, bearer string) (int, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, h.srv.URL+path, &buf)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// The whole point, end to end: an account is lent to a person's machine, the
// machine is told about it, the account is taken back, and the next thing the
// machine hears is that it has nothing.
func TestAMachineIsToldWhatItHoldsAndWhenItStops(t *testing.T) {
	h := newHarness(t)

	if code, body := h.do("POST", "/api/setup", map[string]string{"password": "a-long-enough-one"}, ""); code != 200 {
		t.Fatalf("setup = %d %v", code, body)
	}

	// An account with a login to lend, and someone to lend it to.
	if code, _ := h.do("POST", "/api/accounts", map[string]string{"name": "Work"}, ""); code != 201 {
		t.Fatalf("adding an account = %d", code)
	}
	if code, _ := h.do("POST", "/api/people", map[string]string{"name": "Alice"}, ""); code != 201 {
		t.Fatalf("adding a person = %d", code)
	}
	_, panelBody := h.do("GET", "/api/panel", nil, "")
	accountID := panelBody["accounts"].([]any)[0].(map[string]any)["id"].(string)
	personID := panelBody["people"].([]any)[0].(map[string]any)["id"].(string)

	login := []byte(`{"claudeAiOauth":{"accessToken":"fake"}}`)
	if code, body := h.do("POST", "/api/accounts/"+accountID+"/login",
		map[string]string{"credential": base64.StdEncoding.EncodeToString(login)}, ""); code != 200 {
		t.Fatalf("storing the login = %d %v", code, body)
	}

	// Alice enrols a machine with a one-shot code.
	_, codeBody := h.do("POST", "/api/people/"+personID+"/code", nil, "")
	joinCode, _ := codeBody["code"].(string)
	status, enrolled := h.do("POST", "/api/v1/enroll",
		map[string]string{"code": joinCode, "machine": "alice-mbp"}, "")
	if status != 200 {
		t.Fatalf("enrolling = %d %v", status, enrolled)
	}
	token, _ := enrolled["token"].(string)
	if token == "" {
		t.Fatal("enrolling returned no token")
	}

	// Nothing yet: enrolled is not the same as entitled.
	if _, body := h.do("POST", "/api/v1/checkin", nil, token); body["assignments"] != nil {
		t.Errorf("a machine with no assignment was told it holds %v", body["assignments"])
	}

	if code, body := h.do("POST", "/api/assign",
		map[string]any{"accountId": accountID, "personId": personID, "hours": 24}, ""); code != 200 {
		t.Fatalf("assigning = %d %v", code, body)
	}

	_, body := h.do("POST", "/api/v1/checkin", nil, token)
	list, _ := body["assignments"].([]any)
	if len(list) != 1 {
		t.Fatalf("the machine was told it holds %d accounts, want 1", len(list))
	}
	got := list[0].(map[string]any)
	if got["name"] != "Work" {
		t.Errorf("assignment name = %v, want Work", got["name"])
	}
	// The login travels with it, and is the one that was escrowed.
	raw, err := base64.StdEncoding.DecodeString(got["credential"].(string))
	if err != nil || string(raw) != string(login) {
		t.Errorf("credential handed to the machine = %q (%v), want the stored login", raw, err)
	}

	// Taken back.
	_, panelBody = h.do("GET", "/api/panel", nil, "")
	assignmentID := panelBody["accounts"].([]any)[0].(map[string]any)["assignmentId"].(string)
	if code, _ := h.do("POST", "/api/assignments/"+assignmentID+"/takeback", nil, ""); code != 200 {
		t.Fatalf("taking it back = %d", code)
	}

	// The next check-in is the machine finding out. A complete list, so an
	// empty one means "let go of everything".
	_, body = h.do("POST", "/api/v1/checkin", nil, token)
	if list, _ := body["assignments"].([]any); len(list) != 0 {
		t.Errorf("after being taken back the machine still holds %v", list)
	}
}

func TestThePanelIsClosedToStrangers(t *testing.T) {
	h := newHarness(t)
	if code, _ := h.do("POST", "/api/setup", map[string]string{"password": "a-long-enough-one"}, ""); code != 200 {
		t.Fatal("setup failed")
	}
	// A fresh client: signed in nowhere.
	h.client = &http.Client{Jar: &cookieJar{}}

	for _, path := range []string{"/api/panel"} {
		if code, _ := h.do("GET", path, nil, ""); code != 401 {
			t.Errorf("GET %s without signing in = %d, want 401", path, code)
		}
	}
	if code, _ := h.do("POST", "/api/accounts", map[string]string{"name": "Sneaky"}, ""); code != 401 {
		t.Errorf("adding an account without signing in = %d, want 401", code)
	}
	if code, _ := h.do("POST", "/api/login", map[string]string{"password": "wrong"}, ""); code != 401 {
		t.Errorf("signing in with the wrong password = %d, want 401", code)
	}
	if code, _ := h.do("POST", "/api/login", map[string]string{"password": "a-long-enough-one"}, ""); code != 200 {
		t.Errorf("signing in with the right password = %d, want 200", code)
	}
	if code, _ := h.do("GET", "/api/panel", nil, ""); code != 200 {
		t.Errorf("reading the panel after signing in = %d, want 200", code)
	}
}

// Cutting off a machine has to be immediate and total, whatever it still holds.
func TestACutOffMachineIsTurnedAway(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/setup", map[string]string{"password": "a-long-enough-one"}, "")
	h.do("POST", "/api/people", map[string]string{"name": "Bob"}, "")
	_, panelBody := h.do("GET", "/api/panel", nil, "")
	personID := panelBody["people"].([]any)[0].(map[string]any)["id"].(string)

	_, codeBody := h.do("POST", "/api/people/"+personID+"/code", nil, "")
	_, enrolled := h.do("POST", "/api/v1/enroll",
		map[string]any{"code": codeBody["code"], "machine": "bob-pc"}, "")
	token := enrolled["token"].(string)
	deviceID := enrolled["deviceId"].(string)

	if code, _ := h.do("POST", "/api/v1/checkin", nil, token); code != 200 {
		t.Fatal("an enrolled machine could not check in")
	}
	if code, _ := h.do("DELETE", "/api/devices/"+deviceID, nil, ""); code != 204 {
		t.Fatal("cutting the machine off failed")
	}
	if code, body := h.do("POST", "/api/v1/checkin", nil, token); code != 401 {
		t.Errorf("a cut-off machine checked in: %d %v", code, body)
	}
}

// A minimal cookie jar: net/http/cookiejar needs a public-suffix list to accept
// cookies for "127.0.0.1", and this only has to hold one.
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *neturl.URL, cookies []*http.Cookie) { j.cookies = cookies }
func (j *cookieJar) Cookies(_ *neturl.URL) []*http.Cookie             { return j.cookies }
