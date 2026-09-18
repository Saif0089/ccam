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
	"strconv"
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

// Resolution is what a member key maps to: the subscription token to forward
// with, a log label (never the token), and the identity — account + person —
// the key belongs to, which the gateway attributes usage to.
type Resolution struct {
	AccessToken string
	Label       string
	AccountID   string
	PersonID    string
}

// Upstream identifies which subscription a member's request should be served by.
type Upstream interface {
	// Resolve maps a member's gateway key to its Resolution. A nil error means
	// forward; ErrUnknownKey means answer 401; any other error means the share is
	// valid but its login is unusable right now, answered with 502.
	Resolve(memberKey string) (Resolution, error)
}

// Limiter reports whether a member is over quota, so the gateway can answer 429
// before forwarding — the same shape a real spend limit uses. Optional; nil
// means no quotas are enforced.
type Limiter interface {
	// OverLimit returns whether this person is over quota now, and if so a
	// message and how many seconds until the window resets (for retry-after).
	OverLimit(personID string) (over bool, retryAfter int, message string)
}

// New builds the gateway handler over an Upstream. rec, if non-nil, meters each
// forwarded response off the hot path; lim, if non-nil, is checked before each
// forward and answers over-quota members with a 429.
func New(up Upstream, rec Recorder, lim Limiter) http.Handler {
	proxy := &httputil.ReverseProxy{
		// -1 flushes every write immediately, which is what keeps streamed
		// (SSE) responses streaming instead of buffering to the end.
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			if rec == nil || resp.Body == nil {
				return nil
			}
			ident, _ := resp.Request.Context().Value(identKey).(Event)
			ident.RequestID = resp.Header.Get("request-id")
			resp.Body = &meteringBody{inner: resp.Body, rec: rec, base: ident}
			return nil
		},
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
		res, err := up.Resolve(key)
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
		// Quota gate: an over-cap member is turned away here, before their request
		// reaches Anthropic, with a definitive 429 pointing at the window reset.
		if lim != nil && res.PersonID != "" {
			if over, retryAfter, msg := lim.OverLimit(res.PersonID); over {
				denyQuota(w, retryAfter, msg)
				return
			}
		}
		ctx := withToken(r.Context(), res.AccessToken)
		ctx = context.WithValue(ctx, identKey, Event{AccountID: res.AccountID, PersonID: res.PersonID})
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}

// denyQuota answers an over-quota member with the shape a real spend limit uses:
// 429, error.type billing_error, no retry, and a retry-after at the reset — so
// Claude Code shows the message and stops rather than retrying into the cap.
func denyQuota(w http.ResponseWriter, retryAfterSec int, message string) {
	body, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": "billing_error", "message": message},
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-should-retry", "false")
	if retryAfterSec > 0 {
		w.Header().Set("retry-after", strconv.Itoa(retryAfterSec))
	}
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write(body)
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

const (
	tokenKey ctxKey = iota
	identKey        // carries the resolved Event{AccountID,PersonID} for metering
)

func withToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}
