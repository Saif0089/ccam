package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"ccam/internal/claudebin"
)

// cmdUse runs Claude Code through a ccam gateway: it points Claude at the
// gateway (ANTHROPIC_BASE_URL) and presents the person's key
// (ANTHROPIC_AUTH_TOKEN), so their traffic is served by the shared subscription
// the gateway holds — the person never has the credential, and many people can
// use one account at once.
//
//	ccam use <gateway-url> <key> [claude args...]
func cmdUse(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ccam use <gateway-url> <key> [claude args...]")
		return 2
	}
	url, key := strings.TrimRight(args[0], "/"), args[1]
	rest := args[2:]

	name, argv := claudebin.Invocation(claudebin.Resolve(), rest)
	cmd := exec.Command(name, argv...)
	// Gateway mode. CLAUDE_CONFIG_DIR and the securestorage var are stripped so a
	// stray local login can't take precedence over the gateway.
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_BASE_URL="+url,
		"ANTHROPIC_AUTH_TOKEN="+key,
	)
	cmd.Env = accountsEnvWithout(cmd.Env, "CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return exitCodeOf(err)
	}
	return 0
}

// accountsEnvWithout drops the named vars (case-insensitive) from an env slice.
func accountsEnvWithout(env []string, names ...string) []string {
	drop := map[string]bool{}
	for _, n := range names {
		drop[strings.ToUpper(n)] = true
	}
	out := env[:0]
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 && drop[strings.ToUpper(kv[:i])] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
