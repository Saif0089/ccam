package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"ccam/internal/claudebin"
	"ccam/internal/config"
	"ccam/panel"
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
	return runGateway(strings.TrimRight(args[0], "/"), args[1], args[2:])
}

// cmdShared runs a gateway-shared account by its slug, reading the gateway URL
// and this person's key from ccam's shares cache — so nobody has to copy a key
// around, and the shell alias `claude-<slug>` is all that a shared account needs.
//
//	ccam shared <slug> [claude args...]
func cmdShared(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ccam shared <account> [claude args...]")
		return 2
	}
	slug, rest := args[0], args[1:]

	path, err := config.SharesFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	shares, err := panel.LoadShares(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	for _, sh := range shares {
		if strings.EqualFold(sh.Slug, slug) {
			return runGateway(strings.TrimRight(sh.Gateway, "/"), sh.Key, rest)
		}
	}
	fmt.Fprintf(os.Stderr, "ccam: no shared account called %q on this machine.\n", slug)
	if len(shares) > 0 {
		names := make([]string, len(shares))
		for i, sh := range shares {
			names[i] = sh.Slug
		}
		fmt.Fprintf(os.Stderr, "      Shared with you: %s.\n", strings.Join(names, ", "))
	} else {
		fmt.Fprintln(os.Stderr, "      Nothing is shared with this machine yet. Connect to a panel from the ccam page.")
	}
	return 1
}

// runGateway launches Claude Code in gateway mode: it points Claude at the
// gateway (ANTHROPIC_BASE_URL) and presents the key (ANTHROPIC_AUTH_TOKEN).
// CLAUDE_CONFIG_DIR and the securestorage var are stripped so a stray local
// login can't take precedence over the gateway.
func runGateway(url, key string, rest []string) int {
	name, argv := claudebin.Invocation(claudebin.Resolve(), rest)
	cmd := exec.Command(name, argv...)
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
