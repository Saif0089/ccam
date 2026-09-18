package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/panel"
)

// cmdList prints every account this machine can run and the exact command for
// each — the terminal counterpart of the web page's two lists. It is the answer
// to "what do I have here and how do I start it", with nothing to remember.
//
//	ccam list
func cmdList(_ []string) int {
	local, err := loadAccounts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	shares := sharedAccounts()

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', 0)
	fmt.Fprintln(w, "ON THIS MACHINE")
	if len(local) == 0 {
		fmt.Fprintf(w, "  (none yet — sign one in at http://127.0.0.1:%d)\n", config.DefaultPort)
	}
	for _, a := range local {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", displayName(a), statusWord(a), runCommand(a))
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "SHARED WITH YOU")
	switch {
	case len(shares) > 0:
		for _, sh := range shares {
			fmt.Fprintf(w, "  %s\tready\tccam shared %s\n", sh.Account, sh.Slug)
		}
	case panelConnected():
		fmt.Fprintln(w, "  (nothing shared with you yet)")
	default:
		fmt.Fprintf(w, "  (not connected — paste an invite link at http://127.0.0.1:%d)\n", config.DefaultPort)
	}
	w.Flush()
	return 0
}

// runCommand is the command that starts an account: plain `claude` for the
// machine's main login, `ccam <slug>` for every other one.
func runCommand(a accounts.Account) string {
	if a.IsDefault() {
		return "claude"
	}
	return "ccam " + a.Slug
}

// statusWord says whether an account will work right now, in the page's words.
func statusWord(a accounts.Account) string {
	if a.Status == accounts.StatusLinked {
		return "ready"
	}
	return "not signed in"
}

// panelConnected reports whether this machine is enrolled with a panel.
func panelConnected() bool {
	path, err := config.PanelClientFile()
	if err != nil {
		return false
	}
	cfg, err := panel.LoadClientConfig(path)
	return err == nil && cfg.Configured()
}
