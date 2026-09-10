package shellrc

import "testing"

// Every shell must get the same deal: a plain `claude` is supervised by ccam,
// and every reason that cannot work falls through to Claude Code itself. A
// wrapper that could not fall through would leave a shell unable to run claude
// at all the moment ccam was uninstalled.
func TestRenderBodyWrapsPlainClaudeOnEveryShell(t *testing.T) {
	entries := []AliasEntry{{Alias: "claude-work", ConfigDir: "/home/me/.ccam/accounts/work"}}

	for _, shell := range []Shell{Bash, BashLogin, Zsh, Fish, PowerShell, PowerShellDesktop} {
		body := RenderBody(shell, entries)
		for _, want := range []string{
			"ccam run --auto", // supervised, so `!ccam <name>` can switch it
			"CLAUDECODE",      // but not inside a session
			"CCAM_WRAP",       // and there is an escape hatch
		} {
			if !contains(body, want) {
				t.Errorf("%s: wrapper missing %q: %s", shell, want, body)
			}
		}
	}
}
