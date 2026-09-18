package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeUpstream struct{ key, token string }

func (f fakeUpstream) Resolve(k string) (string, string, bool) {
	if k == f.key {
		return f.token, "acct", true
	}
	return "", "", false
}

// The gateway must swap a member's key for the real subscription token and add
// the headers a subscription request needs — without the member ever seeing the
// token. This stands a fake Anthropic up and checks what actually arrives.
func TestGatewaySwapsInTheSubscriptionToken(t *testing.T) {
	var gotAuth, gotBeta, gotVersion string
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotVersion = r.Header.Get("anthropic-version")
		io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer anthropic.Close()

	// Point the gateway's target at the fake by overriding the host it dials.
	h := New(fakeUpstream{key: "member-key", token: "REAL-SUB-TOKEN"})
	srv := httptest.NewServer(rewriteHost(h, anthropic.Listener.Addr().String()))
	defer srv.Close()

	// A client in gateway mode presents its member key and its own betas.
	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer member-key")
	req.Header.Set("anthropic-beta", "tool-search-2025-10-19")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotAuth != "Bearer REAL-SUB-TOKEN" {
		t.Errorf("Anthropic saw Authorization %q, want the subscription token (member key must not leak)", gotAuth)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q", gotVersion)
	}
	if !strings.Contains(gotBeta, "oauth-2025-04-20") || !strings.Contains(gotBeta, "tool-search-2025-10-19") {
		t.Errorf("anthropic-beta = %q, want the oauth beta added AND the client's beta kept", gotBeta)
	}
}

func TestGatewayRejectsUnknownKey(t *testing.T) {
	h := New(fakeUpstream{key: "good", token: "t"})
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, key := range []string{"", "bad"} {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != 401 {
			t.Errorf("key %q -> %d, want 401", key, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// rewriteHost points the proxy's outbound host at the test's fake Anthropic,
// since New hardcodes the real host.
func rewriteHost(h http.Handler, host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testTargetHost = host
		h.ServeHTTP(w, r)
	})
}
