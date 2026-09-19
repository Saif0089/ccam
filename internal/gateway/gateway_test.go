package gateway

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// capRec captures the one metering Event the gateway records.
type capRec struct{ ch chan Event }

func (c *capRec) Record(e Event) {
	select {
	case c.ch <- e:
	default:
	}
}

// The gateway must meter a streamed (SSE) response: pull the model and input
// tokens from message_start and the final output tokens from message_delta,
// attributed to the member's account+person, without buffering the stream.
func TestGatewayMetersAStreamedResponse(t *testing.T) {
	sse := "event: message_start\n" +
		`data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":100,"cache_creation_input_tokens":10,"cache_read_input_tokens":5,"output_tokens":1}}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":42}}` + "\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("request-id", "req_abc")
		io.WriteString(w, sse)
	}))
	defer anthropic.Close()

	rec := &capRec{ch: make(chan Event, 1)}
	h := New(fakeUpstream{key: "member-key", token: "T"}, rec, nil)
	srv := httptest.NewServer(rewriteHost(h, anthropic.Listener.Addr().String()))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer member-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body) // drain so the metering body reaches EOF
	resp.Body.Close()

	select {
	case ev := <-rec.ch:
		if ev.Model != "claude-sonnet-4-6" {
			t.Errorf("model = %q", ev.Model)
		}
		if ev.Input != 100 || ev.Output != 42 || ev.CacheCreation != 10 || ev.CacheRead != 5 {
			t.Errorf("tokens = in %d out %d cc %d cr %d, want 100/42/10/5", ev.Input, ev.Output, ev.CacheCreation, ev.CacheRead)
		}
		if ev.AccountID != "acct-1" || ev.PersonID != "person-1" {
			t.Errorf("attribution = account %q person %q, want acct-1/person-1", ev.AccountID, ev.PersonID)
		}
		if ev.RequestID != "req_abc" {
			t.Errorf("request id = %q, want req_abc", ev.RequestID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway recorded no usage for a streamed response")
	}
}

// fixedLimiter reports the same quota standing for every member, to drive the
// deny and warn paths.
type fixedLimiter struct{ st QuotaStatus }

func (f fixedLimiter) Status(string) QuotaStatus { return f.st }

// An over-quota member is turned away with a 429 billing_error before the
// request ever reaches Anthropic — the shape a real spend limit uses.
func TestGatewayRejectsOverQuotaWith429(t *testing.T) {
	over := fixedLimiter{QuotaStatus{
		Over: true, Fraction: 1.0,
		ResetAt: time.Now().Add(time.Hour),
		Message: "your clawdh daily quota is reached; it resets soon.",
	}}
	h := New(fakeUpstream{key: "member-key", token: "T"}, nil, over)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer member-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Errorf("over quota -> %d, want 429", resp.StatusCode)
	}
	if ra, _ := strconv.Atoi(resp.Header.Get("retry-after")); ra < 3500 || ra > 3600 {
		t.Errorf("retry-after = %q, want ~3600 (until the window reset)", resp.Header.Get("retry-after"))
	}
	if resp.Header.Get("x-should-retry") != "false" {
		t.Error("a quota 429 must tell Claude Code not to retry it")
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "billing_error") {
		t.Errorf("body = %s, want a billing_error", body)
	}
}

// A member who is forwarded but past 75% of their clawdh quota gets an
// allowed_warning on the way back — the signal Claude Code turns into an
// "approaching usage limit" notice — and the upstream's own rate-limit headers,
// which describe the shared login as a whole, are stripped in favour of it.
func TestGatewayWarnsApproachingQuota(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour)
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The upstream reports its own (whole-login) utilization; clawdh replaces it.
		w.Header().Set("anthropic-ratelimit-unified-status", "allowed")
		w.Header().Set("anthropic-ratelimit-unified-5h-utilization", "12")
		io.WriteString(w, `{"type":"message","content":[]}`)
	}))
	defer anthropic.Close()
	testTargetHost = strings.TrimPrefix(anthropic.URL, "http://")
	defer func() { testTargetHost = "" }()

	warn := fixedLimiter{QuotaStatus{Fraction: 0.82, ResetAt: reset,
		Message: "your clawdh daily quota is reached; it resets soon."}}
	srv := httptest.NewServer(New(fakeUpstream{key: "member-key", token: "T"}, nil, warn))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer member-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("approaching-quota member -> %d, want 200 (still served)", resp.StatusCode)
	}
	if got := resp.Header.Get("anthropic-ratelimit-unified-status"); got != "allowed_warning" {
		t.Errorf("unified-status = %q, want allowed_warning at 82%%", got)
	}
	if got := resp.Header.Get("anthropic-ratelimit-unified-reset"); got != strconv.FormatInt(reset.Unix(), 10) {
		t.Errorf("unified-reset = %q, want the window reset %d", got, reset.Unix())
	}
	if resp.Header.Get("anthropic-ratelimit-unified-5h-utilization") != "" {
		t.Error("the upstream's own unified rate-limit headers must be stripped")
	}
}

type fakeUpstream struct{ key, token string }

func (f fakeUpstream) Resolve(k string) (Resolution, error) {
	if k == f.key {
		return Resolution{AccessToken: f.token, Label: "acct", AccountID: "acct-1", PersonID: "person-1"}, nil
	}
	return Resolution{}, ErrUnknownKey
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
	h := New(fakeUpstream{key: "member-key", token: "REAL-SUB-TOKEN"}, nil, nil)
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
	h := New(fakeUpstream{key: "good", token: "t"}, nil, nil)
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

// A share that is real but whose login can't produce a token is a 502, not a
// 401: the member did nothing wrong, so telling them their access was withdrawn
// would be a lie and a retry might succeed.
func TestGatewayReports502WhenTheLoginIsUnusable(t *testing.T) {
	h := New(brokenUpstream{}, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer anything")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Errorf("a valid key with an unusable login -> %d, want 502", resp.StatusCode)
	}
	if resp.Header.Get("x-should-retry") != "false" {
		t.Error("a definitive gateway error must tell Claude Code not to retry it")
	}
}

// brokenUpstream stands for a share whose subscription login cannot be refreshed
// right now — a known key, but no token.
type brokenUpstream struct{}

func (brokenUpstream) Resolve(string) (Resolution, error) {
	return Resolution{}, errors.New("refreshing the shared login: the token service answered 400")
}

// rewriteHost points the proxy's outbound host at the test's fake Anthropic,
// since New hardcodes the real host.
func rewriteHost(h http.Handler, host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testTargetHost = host
		h.ServeHTTP(w, r)
	})
}
