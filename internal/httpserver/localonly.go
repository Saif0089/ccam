package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// withLocalOnly rejects requests that a browser made on behalf of some
// other website, and requests that arrived under a hostname that isn't
// loopback.
//
// Binding to 127.0.0.1 keeps other machines out, but it does not keep
// out the browser already running on this one: any page the user
// visits can POST to http://127.0.0.1:47932. CORS stops it reading the
// response, but every side effect still happens — and ccam's side
// effects include creating accounts, starting `claude` login processes,
// and opening a terminal window running claude. So:
//
//   - Any state-changing request carrying an Origin that isn't ccam's
//     own is refused. Browsers always send Origin on cross-origin
//     requests, including "simple" ones and sendBeacon; a local CLI
//     like curl sends none, and is already as privileged as ccam.
//   - Every request must arrive addressed to a loopback host, which
//     closes DNS rebinding: an attacker-controlled name resolving to
//     127.0.0.1 would otherwise be same-origin to the browser and able
//     to read responses too.
func withLocalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "ccam only serves loopback addresses", http.StatusForbidden)
			return
		}

		if isStateChanging(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
				http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// isLoopbackHost reports whether a Host header names this machine's
// loopback interface.
func isLoopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")

	if strings.EqualFold(name, "localhost") {
		return true
	}
	if ip := net.ParseIP(name); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// originMatchesHost reports whether an Origin header is ccam's own.
func originMatchesHost(origin, host string) bool {
	const httpPrefix = "http://"
	if !strings.HasPrefix(origin, httpPrefix) {
		return false
	}
	originHost := strings.TrimPrefix(origin, httpPrefix)

	// Same port, and a loopback name on both sides. Comparing the whole
	// authority also covers 127.0.0.1 vs localhost, which are different
	// origins to a browser and so never mixed within one page.
	return originHost == host && isLoopbackHost(originHost)
}
