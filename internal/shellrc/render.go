package shellrc

import (
	"fmt"
	"sort"
	"strings"
)

// AliasEntry is one account's alias/config-dir pairing, as rendered into
// a shell rc file.
type AliasEntry struct {
	Alias     string
	ConfigDir string
}

// Shell identifies which rc-file syntax to render.
type Shell string

const (
	Bash       Shell = "bash"
	BashLogin  Shell = "bash-login"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
)

// RenderBody produces the managed block's body (no markers) for the
// given shell and account list, sorted by alias for stable, diff-friendly
// output across regenerations.
func RenderBody(shell Shell, entries []AliasEntry) string {
	sorted := append([]AliasEntry{}, entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Alias < sorted[j].Alias })

	var b strings.Builder
	b.WriteString("# Managed by ccam — do not edit by hand, use the ccam web UI instead.\n")
	for _, e := range sorted {
		switch shell {
		case Fish:
			// The alias value itself is single-quoted, so the directory
			// is double-quoted inside it rather than nesting single
			// quotes (which would terminate the outer quote early).
			fmt.Fprintf(&b, "alias %s 'env CLAUDE_CONFIG_DIR=%s claude'\n", e.Alias, dquote(e.ConfigDir))
		case PowerShell:
			fmt.Fprintf(&b, "function %s { $env:CLAUDE_CONFIG_DIR = %s; claude @args }\n", e.Alias, psQuote(e.ConfigDir))
		default: // bash, zsh
			fmt.Fprintf(&b, "alias %s='CLAUDE_CONFIG_DIR=%s claude'\n", e.Alias, dquote(e.ConfigDir))
		}
	}
	return b.String()
}

// dquote double-quotes s for embedding inside an already single-quoted
// alias value, escaping the characters that are still special inside
// double quotes.
func dquote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
