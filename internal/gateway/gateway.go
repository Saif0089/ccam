// Package gateway is clawdh's data plane: a reverse proxy that lets many people
// share one Claude subscription at once, without any of them ever holding its
// credential.
//
// Each person's Claude Code is pointed at this gateway in "gateway mode"
// (ANTHROPIC_BASE_URL + a per-person key). The gateway authenticates the key,
// swaps in the one subscription's real token, and forwards to Anthropic —
// streaming the reply straight back. The subscription's OAuth token is
// refreshed centrally here, so no client ever rotates it, which is the whole
// reason two live sessions on one login stop colliding.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// ErrUnknownKey means the presented gateway key belongs to no live share: it was
// never issued, or its share was revoked. The gateway answers it with 401, which
// is how revoking a share cuts a member off instantly. Any other error from
// Resolve means the share is real but its subscription login could not produce a
// token right now (a failed central refresh, usually because the same login is
// still being used first-party somewhere) — a 502, not a 401, because the member
// did nothing wrong and retrying may help.
var ErrUnknownKey = errors.New("unknown or revoked gateway key")

const (
	anthropicHost = "api.anthropic.com"
	// oauthBeta is the anthropic-beta value a subscription (OAuth) token needs
	// on /v1/messages — verified live against a real Max login.
	oauthBeta        = "oauth-2025-04-20"
	anthropicVersion = "2023-06-01"
)

// Upstream identifies which subscription a member's request should be served by,
// and returns that subscription's current access token. ok is false when the
// key is unknown or revoked — the gateway then rejects the request, which is how
// revocation takes effect instantly with nothing to reach the member's machine.
type Upstream interface {
	// Resolve maps a member's gateway key to the subscription access token to
	// forward with, plus a label for logging (never the token). A nil error
	// means forward; ErrUnknownKey means answer 401; any other error means the
	// share is valid but its login is unusable right now, answered with 502.
	Resolve(memberKey string) (accessToken, label string, err error)
}

// New builds the gateway handler over an Upstream.
func New(up Upstream) http.Handler {
	target := &url.URL{Scheme: "https", Host: anthropicHost}
	_ = target
	proxy := &httputil.ReverseProxy{
		// -1 flushes every write immediately, which is what keeps streamed
		// (SSE) responses streaming instead of buffering to the end.
		FlushInterval: -1,
		Director: func(r *http.Request) {
			token, _ := r.Context().Value(tokenKey).(string)
			host, scheme := anthropicHost, "https"
			if testTargetHost != "" { // set only by tests
				host, scheme = testTargetHost, "http"
			}
			r.URL.Scheme = scheme
			r.URL.Host = host
			r.Host = host

			// Replace the member's key with the subscription's real credential,
			// and add exactly what a first-party subscription request carries.
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Del("x-api-key")
			// The compatibility guide says to forward anthropic-version and
			// anthropic-beta unchanged; the version is only filled in when the
			// client sent none, and the beta list keeps everything the client
			// asked for plus the OAuth capability a subscription token needs.
			if r.Header.Get("anthropic-version") == "" {
				r.Header.Set("anthropic-version", anthropicVersion)
			}
			r.Header.Set("anthropic-beta", withBeta(r.Header.Get("anthropic-beta"), oauthBeta))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := memberKey(r)
		if key == "" {
			deny(w, http.StatusUnauthorized, "authentication_error",
				"No gateway key was sent. Run this account through clawdh (`clawdh shared <name>`), which supplies your key.")
			return
		}
		token, _, err := up.Resolve(key)
		switch {
		case errors.Is(err, ErrUnknownKey):
			deny(w, http.StatusUnauthorized, "authentication_error",
				"Your access to this shared account was removed, or this key is not one the gateway knows. Ask whoever shared it to give you access again; `clawdh list` shows what you can run.")
			return
		case err != nil:
			// The share is real but its shared login can't be used right now —
			// almost always because the same account is still signed in and in
			// use first-party somewhere, which rotates the login's refresh token
			// out from under the gateway. Nothing the member does will fix it, so
			// say what will, and don't have the client retry into it.
			deny(w, http.StatusBadGateway, "api_error",
				"The shared login for this account stopped working — usually because the same account is also being used directly on another machine, which invalidates the copy the gateway holds. The account's owner needs to add its login to the panel again.")
			return
		}
		proxy.ServeHTTP(w, r.WithContext(withToken(r.Context(), token)))
	})
}

// deny answers with the Anthropic error envelope Claude Code expects, and with
// x-should-retry: false. Every gateway-issued error here is definitive — a
// missing or revoked key, a login that needs re-adding — so retrying is pure
// delay; without the header Claude Code retries a 5xx up to ten times with
// backoff before the person ever sees the message.
func deny(w http.ResponseWriter, status int, errType, message string) {
	body, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": message},
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-should-retry", "false")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// memberKey pulls the caller's gateway key from where Claude Code puts it in
// gateway mode: a Bearer Authorization (ANTHROPIC_AUTH_TOKEN) or x-api-key
// (ANTHROPIC_API_KEY).
func memberKey(r *http.Request) string {
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(a, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

// withBeta ensures need is present in a comma-separated anthropic-beta header,
// keeping whatever betas Claude Code already asked for.
func withBeta(have, need string) string {
	if have == "" {
		return need
	}
	for _, b := range strings.Split(have, ",") {
		if strings.TrimSpace(b) == need {
			return have
		}
	}
	return have + "," + need
}

// testTargetHost redirects the proxy to a stand-in Anthropic in tests; empty in production.
var testTargetHost string

type ctxKey int

const tokenKey ctxKey = 0

func withToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}
