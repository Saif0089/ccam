package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCrossOriginStateChangesAreRefused covers the reason this
// middleware exists: binding to 127.0.0.1 keeps other machines out, but
// not the browser on this one. Any page the user visits could otherwise
// create accounts and open a terminal running claude — CORS hides the
// response, but the side effects still happen.
func TestCrossOriginStateChangesAreRefused(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()

	body := []byte(`{"name":"Drive By"}`)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47932/api/accounts", bytes.NewReader(body))
	req.Host = "127.0.0.1:47932"
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; a website could create accounts", rec.Code)
	}
}

func TestCrossOriginLaunchTerminalIsRefused(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47932/api/accounts/work/launch-terminal", nil)
	req.Host = "127.0.0.1:47932"
	req.Header.Set("Origin", "https://evil.example.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; a website could open a terminal", rec.Code)
	}
}

func TestSameOriginRequestsAreAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()

	body := []byte(`{"name":"Work"}`)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47932/api/accounts", bytes.NewReader(body))
	req.Host = "127.0.0.1:47932"
	req.Header.Set("Origin", "http://127.0.0.1:47932")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 for a same-origin request", rec.Code)
	}
}

// A local CLI (curl, a script) sends no Origin and is already as
// privileged as ccam itself, so it must keep working.
func TestRequestsWithoutAnOriginAreAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()

	body := []byte(`{"name":"From A Script"}`)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47932/api/accounts", bytes.NewReader(body))
	req.Host = "127.0.0.1:47932"
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 for a local script", rec.Code)
	}
}

// TestNonLoopbackHostIsRefused closes DNS rebinding: a name the
// attacker controls that resolves to 127.0.0.1 would be same-origin to
// the browser, and could then read responses too.
func TestNonLoopbackHostIsRefused(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "http://ccam.evil.example.com:47932/api/accounts", nil)
	req.Host = "ccam.evil.example.com:47932"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a non-loopback Host", rec.Code)
	}
}

func TestLoopbackHostVariantsAreAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()

	for _, host := range []string{"127.0.0.1:47932", "localhost:47932", "[::1]:47932"} {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/accounts", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("host %s: status = %d, want 200", host, rec.Code)
		}
	}
}
