package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The page asks this on every poll; unenrolled it must simply say so, not error.
func TestPanelStatusUnenrolled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/panel")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/panel = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Enrolled bool `json:"enrolled"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Enrolled {
		t.Error("a machine with no panel config reports enrolled=true")
	}
}
