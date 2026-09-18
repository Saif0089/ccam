// Command ccam-gateway runs the gateway data plane. This first cut serves one
// subscription with a static member key, to prove the client->gateway->Anthropic
// path end to end; the DB-backed multi-account version builds on it.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"ccam/internal/gateway"
)

type staticUpstream struct{ key, token string }

func (s staticUpstream) Resolve(k string) (string, string, bool) {
	if k != "" && k == s.key {
		return s.token, "static", true
	}
	return "", "", false
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address")
	memberKey := flag.String("member-key", os.Getenv("CCAM_GW_MEMBER_KEY"), "the gateway key a client presents")
	flag.Parse()

	token := os.Getenv("CCAM_GW_TOKEN")
	if token == "" || *memberKey == "" {
		fmt.Fprintln(os.Stderr, "set CCAM_GW_TOKEN (subscription access token) and --member-key")
		os.Exit(2)
	}
	h := gateway.New(staticUpstream{key: *memberKey, token: token})
	fmt.Printf("ccam-gateway on http://%s (forwarding to api.anthropic.com)\n", *addr)
	if err := http.ListenAndServe(*addr, h); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
