package panel

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// joinCodeLife is how long a join code is good for. One-shot and short-lived is
// what makes a code short enough to read out over a call safe to use at all.
const joinCodeLife = 24 * time.Hour

// ---------------------------------------------------------------- the panel

type accountView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email,omitempty"`
	Plan         string     `json:"plan,omitempty"`
	HasLogin     bool       `json:"hasLogin"`
	Holder       string     `json:"holder,omitempty"`
	HolderID     string     `json:"holderId,omitempty"`
	AssignmentID string     `json:"assignmentId,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}

type deviceView struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	LastSeen *time.Time `json:"lastSeen,omitempty"`
}

type personView struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Email   string       `json:"email,omitempty"`
	Holds   []string     `json:"holds,omitempty"`
	Devices []deviceView `json:"devices,omitempty"`
}

// handlePanel builds the whole interface in one read. Three tabs over one
// answer is far less to keep in step than three endpoints that can disagree.
func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	// A read that settles expiry first, so the panel never shows an account as
	// out when its time has already run out.
	var d Data
	if err := s.store.Mutate(func(cur *Data) error { d = *cur; return nil }); err != nil {
		fail(w, 500, err.Error())
		return
	}
	now := s.now()

	accounts := make([]accountView, 0, len(d.Accounts))
	for _, a := range d.Accounts {
		v := accountView{ID: a.ID, Name: a.Name, Email: a.Email, Plan: a.Plan, HasLogin: a.HasLogin()}
		if held, ok := d.HolderOf(a.ID, now); ok {
			v.Holder, v.HolderID, v.AssignmentID = d.personName(held.PersonID), held.PersonID, held.ID
			if !held.ExpiresAt.IsZero() {
				exp := held.ExpiresAt
				v.ExpiresAt = &exp
			}
		}
		accounts = append(accounts, v)
	}

	people := make([]personView, 0, len(d.People))
	for _, p := range d.People {
		v := personView{ID: p.ID, Name: p.Name, Email: p.Email}
		for _, h := range d.Holdings(p.ID, now) {
			v.Holds = append(v.Holds, d.accountName(h.AccountID))
		}
		for _, dev := range d.Devices {
			if dev.PersonID != p.ID {
				continue
			}
			dv := deviceView{ID: dev.ID, Name: dev.Name}
			if !dev.LastSeen.IsZero() {
				seen := dev.LastSeen
				dv.LastSeen = &seen
			}
			v.Devices = append(v.Devices, dv)
		}
		people = append(people, v)
	}

	activity := append([]Event(nil), d.Activity...)
	sort.Slice(activity, func(i, j int) bool { return activity[i].At.After(activity[j].At) })
	if len(activity) > 200 {
		activity = activity[:200]
	}

	writeJSON(w, 200, map[string]any{"accounts": accounts, "people": people, "activity": activity})
}

// ---------------------------------------------------------------- accounts

func (s *Server) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Plan  string `json:"plan"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		fail(w, 400, "Give the account a name.")
		return
	}
	err := s.store.Mutate(func(d *Data) error {
		for _, a := range d.Accounts {
			if strings.EqualFold(a.Name, in.Name) {
				return fmt.Errorf("There is already an account called %s.", in.Name)
			}
		}
		d.Accounts = append(d.Accounts, Account{
			ID: newID(), Name: in.Name, Email: in.Email, Plan: in.Plan, CreatedAt: s.now(),
		})
		d.Log(s.now(), "You", "added "+in.Name)
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}

// handleStoreLogin escrows an account's Claude login.
//
// The panel takes the credentials file Claude Code wrote for an account that is
// already signed in, rather than driving a sign-in itself: an admin runs
// `ccam panel push <account>` on the machine where that account is linked. It
// is sealed before it touches disk.
func (s *Server) handleStoreLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Credential string `json:"credential"` // base64 of the credentials JSON
		Email      string `json:"email"`
		Plan       string `json:"plan"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "That request could not be read.")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(in.Credential)
	if err != nil || len(raw) == 0 {
		fail(w, 400, "That does not look like a stored login.")
		return
	}
	sealed, err := s.secret.Seal(raw)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	id := r.PathValue("id")
	err = s.store.Mutate(func(d *Data) error {
		a, ok := d.Account(id)
		if !ok {
			return errors.New("There is no such account.")
		}
		a.Credential = sealed
		if in.Email != "" {
			a.Email = in.Email
		}
		if in.Plan != "" {
			a.Plan = in.Plan
		}
		d.Log(s.now(), "You", "stored the login for "+a.Name)
		return nil
	})
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.Mutate(func(d *Data) error {
		a, ok := d.Account(id)
		if !ok {
			return errors.New("There is no such account.")
		}
		name := a.Name
		// Anyone holding it loses it, and the log says why rather than leaving
		// a machine wiping a login for no stated reason.
		now := s.now()
		for i := range d.Assignments {
			if d.Assignments[i].AccountID == id && d.Assignments[i].Active(now) {
				d.end(&d.Assignments[i], now, EndedTakenBack)
			}
		}
		out := d.Accounts[:0]
		for _, acct := range d.Accounts {
			if acct.ID != id {
				out = append(out, acct)
			}
		}
		d.Accounts = out
		d.Log(now, "You", "removed "+name)
		return nil
	})
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ---------------------------------------------------------------- people

func (s *Server) handleAddPerson(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		fail(w, 400, "Give the person a name.")
		return
	}
	err := s.store.Mutate(func(d *Data) error {
		d.People = append(d.People, Person{ID: newID(), Name: in.Name, Email: in.Email, CreatedAt: s.now()})
		d.Log(s.now(), "You", "added "+in.Name)
		return nil
	})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}

func (s *Server) handleRemovePerson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.Mutate(func(d *Data) error {
		p, ok := d.Person(id)
		if !ok {
			return errors.New("There is no such person.")
		}
		name := p.Name
		now := s.now()
		for i := range d.Assignments {
			if d.Assignments[i].PersonID == id && d.Assignments[i].Active(now) {
				d.end(&d.Assignments[i], now, EndedPersonGone)
			}
		}
		devices := d.Devices[:0]
		for _, dev := range d.Devices {
			if dev.PersonID != id {
				devices = append(devices, dev)
			}
		}
		d.Devices = devices
		people := d.People[:0]
		for _, per := range d.People {
			if per.ID != id {
				people = append(people, per)
			}
		}
		d.People = people
		d.Log(now, "You", "removed "+name+", and everything they were holding came back")
		return nil
	})
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleJoinCode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	code, hash, err := NewJoinCode()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	expires := s.now().Add(joinCodeLife)
	err = s.store.Mutate(func(d *Data) error {
		p, ok := d.Person(id)
		if !ok {
			return errors.New("There is no such person.")
		}
		d.JoinCodes = append(d.JoinCodes, JoinCode{CodeHash: hash, PersonID: id, ExpiresAt: expires})
		d.Log(s.now(), "You", "made a join code for "+p.Name)
		return nil
	})
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"code": code, "expiresAt": expires})
}

func (s *Server) handleRemoveDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.Mutate(func(d *Data) error {
		dev, ok := d.Device(id)
		if !ok {
			return errors.New("There is no such machine.")
		}
		name, person := dev.Name, d.personName(dev.PersonID)
		out := d.Devices[:0]
		for _, x := range d.Devices {
			if x.ID != id {
				out = append(out, x)
			}
		}
		d.Devices = out
		d.Log(s.now(), "You", fmt.Sprintf("cut off %s, %s's machine", name, person))
		return nil
	})
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ---------------------------------------------------------------- lending

// handleShare gives a person gateway access to an account and returns the
// gateway key to hand to their machine (shown once). Many people can be shared
// one account — that is the gateway model.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PersonID string `json:"personId"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "That request could not be read.")
		return
	}
	id := r.PathValue("id")
	if d, err := s.store.Load(); err == nil {
		if a, ok := d.Account(id); ok && !a.HasLogin() {
			fail(w, http.StatusBadRequest, a.Name+" has no login yet — authenticate it before sharing.")
			return
		}
	}
	key, err := s.store.IssueShare(id, in.PersonID, func(k string) []byte { sealed, _ := s.secret.Seal([]byte(k)); return sealed })
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "gateway": gatewayURL()})
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeShare(r.PathValue("id")); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// gatewayURL is where members route their Claude Code, set on the panel's env.
func gatewayURL() string { return strings.TrimRight(os.Getenv("CCAM_GATEWAY_URL"), "/") }

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AccountID string `json:"accountId"`
		PersonID  string `json:"personId"`
		Hours     int    `json:"hours"` // 0: until it is taken back
		Move      bool   `json:"move"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "That request could not be read.")
		return
	}
	var until time.Time
	if in.Hours > 0 {
		until = s.now().Add(time.Duration(in.Hours) * time.Hour)
	}
	// An account with no login is nothing to lend — refuse rather than hand a
	// member an empty account, which is exactly the confusing state this had.
	if d, err := s.store.Load(); err == nil {
		if a, ok := d.Account(in.AccountID); ok && !a.HasLogin() {
			fail(w, 400, a.Name+" has no login yet. Authenticate it first: on the machine where it is signed in, run `ccam panel push "+a.Name+" <panel-url>`.")
			return
		}
	}
	if _, err := s.store.Assign(in.AccountID, in.PersonID, until, in.Move); err != nil {
		if errors.Is(err, ErrAccountBusy) {
			// Not a failure so much as a question: the interface asks whether
			// to move it, because moving it takes the account off someone.
			fail(w, 409, "Someone else has that account. Moving it will end their access.")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleTakeBack(w http.ResponseWriter, r *http.Request) {
	if err := s.store.TakeBack(r.PathValue("id"), EndedTakenBack, "You"); err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- machines

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code    string `json:"code"`
		Machine string `json:"machine"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "That request could not be read.")
		return
	}
	token, hash, err := NewToken()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	machine := strings.TrimSpace(in.Machine)
	if machine == "" {
		machine = "a machine"
	}
	var deviceID, personName string
	err = s.store.Mutate(func(d *Data) error {
		now := s.now()
		want := HashToken(strings.TrimSpace(in.Code))
		for i := range d.JoinCodes {
			c := &d.JoinCodes[i]
			if !SameToken(want, c.CodeHash) {
				continue
			}
			if !c.UsedAt.IsZero() {
				return errors.New("That code has already been used.")
			}
			if now.After(c.ExpiresAt) {
				return errors.New("That code has expired. Ask for a new one.")
			}
			c.UsedAt = now
			deviceID = newID()
			personName = d.personName(c.PersonID)
			d.Devices = append(d.Devices, Device{
				ID: deviceID, PersonID: c.PersonID, Name: machine,
				TokenHash: hash, EnrolledAt: now, LastSeen: now,
			})
			d.Log(now, d.personName(c.PersonID), "set up "+machine)
			return nil
		}
		return errors.New("That code is not one of ours.")
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"deviceId": deviceID, "token": token, "personName": personName})
}

// clientShare is one account a person may use through the gateway: its name,
// where to route (the gateway URL), and this person's key. The client turns each
// into an alias that runs Claude Code in gateway mode.
type clientShare struct {
	Account string `json:"account"`
	Slug    string `json:"slug"`
	Gateway string `json:"gateway"`
	Key     string `json:"key"`
}

// clientAssignment is one account a machine is entitled to right now.
type clientAssignment struct {
	AccountID  string     `json:"accountId"`
	Name       string     `json:"name"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Credential string     `json:"credential,omitempty"`
}

// handleCheckin is the whole of what a machine asks: what am I entitled to?
//
// The answer is a complete list, not a diff, so anything the machine holds and
// is not told about here is something it must let go of. That is what makes
// taking an account back work without the panel having to reach the machine:
// the machine asks, every half minute, and acts on the answer.
//
// It is deliberately flat. There is no notion here of who decided, or why —
// the machine has no use for it, and a client that does not model the panel
// cannot get the panel's rules wrong.
func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request, dev Device) {
	var out []clientAssignment
	err := s.store.Mutate(func(d *Data) error {
		now := s.now()
		for i := range d.Devices {
			if d.Devices[i].ID == dev.ID {
				d.Devices[i].LastSeen = now
			}
		}
		for _, h := range d.Holdings(dev.PersonID, now) {
			acct, ok := d.Account(h.AccountID)
			if !ok {
				continue
			}
			item := clientAssignment{AccountID: acct.ID, Name: acct.Name}
			if !h.ExpiresAt.IsZero() {
				exp := h.ExpiresAt
				item.ExpiresAt = &exp
			}
			if acct.HasLogin() {
				plain, err := s.secret.Open(acct.Credential)
				if err != nil {
					return err
				}
				item.Credential = base64.StdEncoding.EncodeToString(plain)
			}
			out = append(out, item)
		}
		return nil
	})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	// Gateway shares: the accounts this person may use through the gateway, with
	// their key. This is the model going forward — the credential never leaves
	// the server; the client only learns where to route and its key.
	var shares []clientShare
	if gw := gatewayURL(); gw != "" {
		d, _ := s.store.Load()
		for _, sh := range d.Shares {
			if sh.PersonID != dev.PersonID {
				continue
			}
			acct, ok := d.Account(sh.AccountID)
			if !ok || len(sh.SealedKey) == 0 {
				continue
			}
			plain, err := s.secret.Open(sh.SealedKey)
			if err != nil {
				continue
			}
			shares = append(shares, clientShare{Account: acct.Name, Slug: slugify(acct.Name), Gateway: gw, Key: string(plain)})
		}
	}
	writeJSON(w, 200, map[string]any{"assignments": out, "gateway": shares})
}

// slugify makes a shell-safe short name for an account's alias.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "account"
}
