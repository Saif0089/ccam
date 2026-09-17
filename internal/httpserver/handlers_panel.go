package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"ccam/internal/config"
	"ccam/panel"
)

// The local page's "Team panel" section talks to these. Enrolling a machine and
// seeing what it holds used to be a terminal-only affair (`ccam panel join`);
// this makes it part of the page every account already lives on. The browser
// only ever talks to this local server, which talks to the panel itself — so
// there is no cross-origin call and the device token never reaches the page.

type panelStatus struct {
	Enrolled   bool     `json:"enrolled"`
	Server     string   `json:"server,omitempty"`
	PersonName string   `json:"personName,omitempty"`
	Holdings   []string `json:"holdings,omitempty"`
}

func (s *Server) currentPanelStatus() panelStatus {
	path, err := config.PanelClientFile()
	if err != nil {
		return panelStatus{}
	}
	cfg, err := panel.LoadClientConfig(path)
	if err != nil || !cfg.Configured() {
		return panelStatus{}
	}
	st := panelStatus{Enrolled: true, Server: cfg.Server, PersonName: cfg.PersonName}
	if list, err := s.manager.List(); err == nil {
		for _, a := range list {
			if a.PanelID != "" {
				st.Holdings = append(st.Holdings, a.Name)
			}
		}
	}
	return st
}

func (s *Server) handlePanelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentPanelStatus())
}

// handlePanelConnect enrols this machine with a panel from the page: it takes
// the URL and code, trades them for a device token, saves it, and does one
// check-in so anything already assigned appears at once.
func (s *Server) handlePanelConnect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Server string `json:"server"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "That request could not be read.")
		return
	}
	in.Server, in.Code = strings.TrimSpace(in.Server), strings.TrimSpace(in.Code)
	if in.Server == "" || in.Code == "" {
		writeError(w, http.StatusBadRequest, "Enter the panel address and the join code.")
		return
	}

	path, err := config.PanelClientFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name, _ := os.Hostname()
	if name == "" {
		name = "a machine"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cfg, err := panel.Enroll(ctx, in.Server, in.Code, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := panel.SaveClientConfig(path, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Pull anything already assigned, now, so the page fills immediately.
	client := &panel.Client{Config: cfg, Accounts: s.manager, AfterChange: func() { _ = s.syncAliases() }}
	_, _ = client.CheckIn(ctx)

	writeJSON(w, http.StatusOK, s.currentPanelStatus())
}

// handlePanelDisconnect forgets the panel on this machine. It does not delete
// accounts the panel lent — those are given back the ordinary way, by the panel
// taking them; forgetting the enrolment just stops this machine checking in.
func (s *Server) handlePanelDisconnect(w http.ResponseWriter, r *http.Request) {
	path, err := config.PanelClientFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, panelStatus{})
}

// handlePanelProxy makes the admin panel first-party. It forwards every request
// under /panel/ to the panel this machine is enrolled with, so the panel's own
// UI — the same one it serves on the web, no second copy — runs inside the ccam
// page. Being same-origin with the local server is what makes its login cookie
// work; embedded straight from the web it would be a blocked third-party cookie.
func (s *Server) handlePanelProxy(w http.ResponseWriter, r *http.Request) {
	st := s.currentPanelStatus()
	if !st.Enrolled {
		http.Error(w, "This machine is not connected to a panel.", http.StatusNotFound)
		return
	}
	target, err := url.Parse(st.Server)
	if err != nil {
		http.Error(w, "The panel address is not a valid URL.", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	base := proxy.Director
	proxy.Director = func(req *http.Request) {
		base(req)
		// Strip the /panel prefix so /panel/api/login reaches the panel's
		// /api/login, and send the panel's own host so it routes and its
		// certificate matches.
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/panel")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.Host = target.Host
	}
	proxy.ServeHTTP(w, r)
}
