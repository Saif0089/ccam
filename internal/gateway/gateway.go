// Package gateway is ccam's data plane: a reverse proxy that lets many people
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
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

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
	// forward with, plus a label for logging (never the token).
	Resolve(memberKey string) (accessToken, label string, ok bool)
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
			r.Header.Set("anthropic-version", anthropicVersion)
			r.Header.Set("anthropic-beta", withBeta(r.Header.Get("anthropic-beta"), oauthBeta))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := memberKey(r)
		if key == "" {
			http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"no gateway key"}}`, http.StatusUnauthorized)
			return
		}
		token, _, ok := up.Resolve(key)
		if !ok {
			http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"this access has been withdrawn"}}`, http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r.WithContext(withToken(r.Context(), token)))
	})
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
