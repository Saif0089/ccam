package panel

import (
	"clawdh/internal/config"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed webui/*
var webui embed.FS

// sessionLife is how long an admin stays signed in. Long enough not to be
// irritating, short enough that a browser left open on a shared machine is not
// a standing invitation.
const sessionLife = 12 * time.Hour

// sessionCookie is deliberately not prefixed __Host-: the panel is often run
// over plain http on a private network, and a cookie the browser refuses to
// set there would lock the admin out of their own panel.
const sessionCookie = "clawdh_panel"

// Server is the panel's HTTP surface: an admin UI and API behind a password,
// and a much smaller API that enrolled machines speak.
type Server struct {
	store  *Store
	secret *Secret
	usage  UsageReader // nil for a file-backed panel with no metering DB
	now    func() time.Time
}

// NewServer wires a panel over a store and its key. usage is the metering read
// surface for the boards; pass nil (a local, file-backed panel) to leave the
// usage routes unmounted.
func NewServer(store *Store, secret *Secret, usage UsageReader) *Server {
	return &Server{store: store, secret: secret, usage: usage, now: time.Now}
}

// Handler is the whole panel.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Setting up, and getting in.
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)

	// The admin's own API. One GET builds the whole interface; the rest are
	// the decisions.
	mux.HandleFunc("GET /api/panel", s.admin(s.handlePanel))
	mux.HandleFunc("POST /api/accounts", s.admin(s.handleAddAccount))
	mux.HandleFunc("POST /api/accounts/{id}/login", s.admin(s.handleStoreLogin))
	mux.HandleFunc("DELETE /api/accounts/{id}", s.admin(s.handleRemoveAccount))
	mux.HandleFunc("POST /api/people", s.admin(s.handleAddPerson))
	mux.HandleFunc("DELETE /api/people/{id}", s.admin(s.handleRemovePerson))
	mux.HandleFunc("POST /api/people/{id}/code", s.admin(s.handleJoinCode))
	mux.HandleFunc("POST /api/people/{id}/invite", s.admin(s.handleInvite))
	mux.HandleFunc("DELETE /api/devices/{id}", s.admin(s.handleRemoveDevice))
	mux.HandleFunc("POST /api/accounts/{id}/share", s.admin(s.handleShare))
	mux.HandleFunc("POST /api/shares/{id}/revoke", s.admin(s.handleRevokeShare))

	// The usage boards, when a metering database is wired (the Postgres panel).
	if s.usage != nil {
		mux.HandleFunc("GET /api/usage/people", s.admin(s.handleUsage("person")))
		mux.HandleFunc("GET /api/usage/accounts", s.admin(s.handleUsage("account")))
		mux.HandleFunc("GET /api/usage/burn", s.admin(s.handleBurn))
	}

	// What an enrolled machine speaks.
	mux.HandleFunc("POST /api/v1/enroll", s.handleEnroll)
	mux.HandleFunc("POST /api/v1/checkin", s.device(s.handleCheckin))

	// The public page an invite link opens. No session: it reveals only whether
	// this one code is still good.
	mux.HandleFunc("GET /i/{code}", s.handleInvitePage)

	sub, err := fs.Sub(webui, "webui")
	if err == nil {
		mux.Handle("/", http.FileServerFS(sub))
	}
	return mux
}

// ---------------------------------------------------------------- plumbing

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// fail sends an error the interface can show a person as-is. Panel errors are
// written as sentences for that reason.
func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

// ---------------------------------------------------------------- admin auth

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Load()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"needsSetup":   d.Admin == nil,
		"signedIn":     s.sessionValid(r),
		"canonicalUrl": strings.TrimRight(config.Env("PANEL_URL"), "/"),
		"gatewayUrl":   gatewayURL(),
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "That request could not be read.")
		return
	}
	if len(in.Password) < 10 {
		fail(w, 400, "Use a password of at least 10 characters. This one guards every login the panel lends out.")
		return
	}
	admin, err := SetPassword(in.Password)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	err = s.store.Mutate(func(d *Data) error {
		if d.Admin != nil {
			return errors.New("This panel already has an administrator.")
		}
		d.Admin = &admin
		d.Log(s.now(), "You", "set this panel up")
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	s.startSession(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, "That request could not be read.")
		return
	}
	d, err := s.store.Load()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if d.Admin == nil || !d.Admin.Verify(in.Password) {
		fail(w, 401, "That password is not right.")
		return
	}
	s.startSession(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// startSession seals an expiry into the cookie itself, so nothing about who is
// signed in is kept on the server. That is what lets a panel run as several
// processes at once — a serverless deployment — without an admin signed in on
// one instance being a stranger to the next. The cookie is sealed with the
// panel's own key, so it cannot be forged, and it simply stops working once its
// sealed expiry passes.
func (s *Server) startSession(w http.ResponseWriter) {
	expiry := s.now().Add(sessionLife)
	sealed, err := s.secret.Seal([]byte(expiry.UTC().Format(time.RFC3339)))
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: base64.RawURLEncoding.EncodeToString(sealed), Path: "/",
		Expires: expiry, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionValid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return false
	}
	plain, err := s.secret.Open(sealed)
	if err != nil {
		return false // not sealed by this panel's key
	}
	exp, err := time.Parse(time.RFC3339, string(plain))
	if err != nil {
		return false
	}
	return s.now().Before(exp)
}

// admin guards everything only the administrator may do.
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sessionValid(r) {
			fail(w, 401, "Sign in to the panel first.")
			return
		}
		next(w, r)
	}
}

// device guards what an enrolled machine may do, and hands the handler the
// device that asked.
func (s *Server) device(next func(http.ResponseWriter, *http.Request, Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.TrimSpace(token) == "" {
			fail(w, 401, "This machine is not enrolled with the panel.")
			return
		}
		d, err := s.store.Load()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		want := HashToken(token)
		for _, dev := range d.Devices {
			if SameToken(want, dev.TokenHash) {
				next(w, r, dev)
				return
			}
		}
		// A token that is not recognised is a machine that was removed. Say so
		// plainly: the client turns this into "your access was withdrawn"
		// rather than retrying for ever.
		fail(w, 401, "This machine is no longer enrolled with the panel.")
	}
}
