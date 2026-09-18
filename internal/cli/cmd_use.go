package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"clawdh/internal/claudebin"
	"clawdh/internal/config"
	"clawdh/internal/switching"
	"clawdh/panel"
)

// sharedSessionEnvVar marks a Claude Code process as a shared (gateway) session
// and carries the shared account's name, for the switch hook to read.
const sharedSessionEnvVar = "CLAWDH_SHARED_SESSION"

// cmdUse runs Claude Code through a clawdh gateway: it points Claude at the
// gateway (ANTHROPIC_BASE_URL) and presents the person's key
// (ANTHROPIC_AUTH_TOKEN), so their traffic is served by the shared subscription
// the gateway holds — the person never has the credential, and many people can
// use one account at once.
//
//	clawdh use <gateway-url> <key> [claude args...]
func cmdUse(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: clawdh use <gateway-url> <key> [claude args...]")
		return 2
	}
	return runGateway(strings.TrimRight(args[0], "/"), args[1], "a shared account", args[2:])
}

// cmdShared runs a gateway-shared account by its slug, reading the gateway URL
// and this person's key from clawdh's shares cache — so nobody has to copy a key
// around. Everything after the slug goes to Claude Code unchanged.
//
//	clawdh shared <slug> [claude args...]
func cmdShared(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: clawdh shared <account> [claude args...]")
		return 2
	}
	slug, rest := args[0], args[1:]

	path, err := config.SharesFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdh:", err)
		return 1
	}
	shares, err := panel.LoadShares(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdh:", err)
		return 1
	}
	for _, sh := range shares {
		if strings.EqualFold(sh.Slug, slug) {
			return runGateway(strings.TrimRight(sh.Gateway, "/"), sh.Key, sh.Slug, rest)
		}
	}
	fmt.Fprintf(os.Stderr, "clawdh: no shared account called %q on this machine.\n", slug)
	if len(shares) > 0 {
		names := make([]string, len(shares))
		for i, sh := range shares {
			names[i] = sh.Slug
		}
		fmt.Fprintf(os.Stderr, "      Shared with you: %s.\n", strings.Join(names, ", "))
	} else {
		fmt.Fprintln(os.Stderr, "      Nothing is shared with this machine yet. Connect to a panel from the clawdh page.")
	}
	return 1
}

// runGateway launches Claude Code in gateway mode: it points Claude at the
// gateway (ANTHROPIC_BASE_URL) and presents the key (ANTHROPIC_AUTH_TOKEN).
// CLAUDE_CONFIG_DIR and the securestorage var are stripped so a stray local
// login can't take precedence over the gateway.
//
// label names the shared account for the switch hook: a shared session cannot
// change accounts in place, and the hook says so by name instead of telling the
// person their session "was not started by clawdh". Any supervisor handoff
// inherited from an enclosing `clawdh <account>` session is dropped for the same
// reason — a `clawdh <name>` typed in here must not switch that outer session.
func runGateway(url, key, label string, rest []string) int {
	name, argv := claudebin.Invocation(claudebin.Resolve(), rest)
	cmd := exec.Command(name, argv...)
	env := accountsEnvWithout(os.Environ(),
		"CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR", switching.HandoffEnvVar, sharedSessionEnvVar)
	cmd.Env = append(env,
		"ANTHROPIC_BASE_URL="+url,
		"ANTHROPIC_AUTH_TOKEN="+key,
		sharedSessionEnvVar+"="+label,
	)
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
