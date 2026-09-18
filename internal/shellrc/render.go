package shellrc

import (
	"fmt"
	"sort"
	"strings"
)

// AliasEntry is one account's alias/directory pairing, as rendered into
// a shell rc file.
//
// ConfigDir names the account's private directory, which the rendered
// alias exports as CLAUDE_SECURESTORAGE_CONFIG_DIR: the credentials come
// from there, everything else — sessions, MCP servers, skills, plugins,
// hooks, agents, CLAUDE.md — from the user's own shared ~/.claude. The
// alias also unsets CLAUDE_CONFIG_DIR, because a user who already has it
// exported (exactly the manual workflow ccam replaces) would otherwise
// keep every session isolated and see none of that sharing.
type AliasEntry struct {
	Alias     string
	ConfigDir string
	// Account is what to pass to ccam to start this account — its slug. The
	// entry point prefers `ccam <account>` so the session it starts can be
	// switched from inside; ConfigDir is only used by the fallback that runs
	// Claude Code directly.
	Account string
}

// Shell identifies which rc-file syntax to render.
type Shell string

const (
	Bash       Shell = "bash"
	BashLogin  Shell = "bash-login"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
	// PowerShellDesktop is Windows PowerShell 5.1, the one that ships
	// with Windows. Same syntax, different profile path.
	PowerShellDesktop Shell = "powershell-desktop"
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
		// Without an account name there is nothing to hand ccam, so the entry
		// point is the plain, direct launch it always was. Better a session
		// that cannot be switched than a function that runs `ccam` with no
		// argument.
		if strings.TrimSpace(e.Account) == "" {
			writeDirectEntry(&b, shell, e)
			continue
		}
		// Each account's entry point goes through ccam, so the session it
		// starts is switchable: `ccam <name>` typed in it moves that session
		// and nothing else. The fallback still launches Claude Code directly
		// with only the credential store scoped, so removing ccam — or being
		// somewhere it cannot supervise — leaves a working command behind.
		switch shell {
		case Fish:
			fmt.Fprintf(&b, `function %s
    if set -q CLAUDECODE; or test "$CCAM_WRAP" = 0; or not isatty stdin; or not command -q ccam
        env -u CLAUDE_CONFIG_DIR CLAUDE_SECURESTORAGE_CONFIG_DIR=%s claude $argv
    else
        command ccam %s $argv
    end
end
`, e.Alias, escapeForSingleQuotes(Fish, dquote(e.ConfigDir)), e.Account)
		case PowerShell, PowerShellDesktop:
			// Remove-Item rather than assigning $null or '': the CLI
			// branches on whether the name is present, so a variable
			// left defined-but-empty is not the same as an absent one.
			fmt.Fprintf(&b, `function %s {
    if ($env:CLAUDECODE -or $env:CCAM_WRAP -eq '0' -or [Console]::IsInputRedirected -or
        -not (Get-Command ccam -CommandType Application -ErrorAction SilentlyContinue)) {
        Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue
        $env:CLAUDE_SECURESTORAGE_CONFIG_DIR = %s
        claude @args
    } else {
        ccam %s @args
    }
}
`, e.Alias, psQuote(e.ConfigDir), psQuote(e.Account))
		default: // bash, zsh
			fmt.Fprintf(&b, `%s() {
    if [ -n "$CLAUDECODE" ] || [ "$CCAM_WRAP" = 0 ] || [ ! -t 0 ] || ! command -v ccam >/dev/null 2>&1; then
        env -u CLAUDE_CONFIG_DIR CLAUDE_SECURESTORAGE_CONFIG_DIR=%s command claude "$@"
    else
        command ccam %s "$@"
    fi
}
`, e.Alias, escapeForSingleQuotes(shell, dquote(e.ConfigDir)), e.Account)
		}
	}
	b.WriteString(claudeWrapper(shell))
	return b.String()
}

// writeDirectEntry renders the pre-ccam form: scope the credential store and
// exec Claude Code, with no supervision and no switching.
func writeDirectEntry(b *strings.Builder, shell Shell, e AliasEntry) {
	switch shell {
	case Fish:
		fmt.Fprintf(b, "alias %s 'env -u CLAUDE_CONFIG_DIR CLAUDE_SECURESTORAGE_CONFIG_DIR=%s claude'\n",
			e.Alias, escapeForSingleQuotes(Fish, dquote(e.ConfigDir)))
	case PowerShell, PowerShellDesktop:
		fmt.Fprintf(b, "function %s { Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue; "+
			"$env:CLAUDE_SECURESTORAGE_CONFIG_DIR = %s; claude @args }\n", e.Alias, psQuote(e.ConfigDir))
	default:
		fmt.Fprintf(b, "alias %s='env -u CLAUDE_CONFIG_DIR CLAUDE_SECURESTORAGE_CONFIG_DIR=%s claude'\n",
			e.Alias, escapeForSingleQuotes(shell, dquote(e.ConfigDir)))
	}
}

// claudeWrapper makes a plain `claude` a switchable ccam session, so
// in-session switching does not depend on the user remembering to type
// `ccam <account>` instead. It is a shell function rather than anything on
// PATH: nothing is installed, nothing is shadowed for other tools, and it
// only exists in interactive shells that read this file.
//
// It deliberately steps aside in the cases where supervising would be wrong or
// impossible, falling through to Claude Code itself:
//   - CCAM_WRAP=0, the escape hatch;
//   - inside a Claude Code session (CLAUDECODE), where `claude` means a nested
//     session, not a re-account of the session you are in;
//   - no terminal on stdin — scripts, pipes, CI — where a supervisor has no
//     terminal to relaunch into;
//   - ccam not installed or not on PATH, so removing ccam can never leave a
//     shell unable to run claude.
//
// `command` (and, in PowerShell, -CommandType Application) is what stops the
// fallback from calling this function again.
func claudeWrapper(shell Shell) string {
	switch shell {
	case Fish:
		return `
# A plain ` + "`claude`" + ` is a switchable ccam session; inside it, ` + "`!ccam <name>`" + `
# switches account without leaving the conversation. Falls through to Claude
# Code itself when that cannot work.
function claude
    if set -q CLAUDECODE; or test "$CCAM_WRAP" = 0; or not isatty stdin; or not command -q ccam
        command claude $argv
    else
        command ccam run --auto $argv
    end
end
`
	case PowerShell, PowerShellDesktop:
		return `
# A plain ` + "`claude`" + ` is a switchable ccam session; inside it, ` + "`!ccam <name>`" + `
# switches account without leaving the conversation. The per-account functions
# above call through this one, so they are switchable too.
function claude {
    $real = Get-Command claude -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($env:CLAUDECODE -or $env:CCAM_WRAP -eq '0' -or [Console]::IsInputRedirected -or
        -not (Get-Command ccam -CommandType Application -ErrorAction SilentlyContinue)) {
        if ($real) { & $real.Source @args } else { Write-Error 'claude is not installed' }
    } else {
        ccam run --auto @args
    }
}
`
	default: // bash, bash-login, zsh
		return `
# A plain ` + "`claude`" + ` is a switchable ccam session; inside it, ` + "`!ccam <name>`" + `
# switches account without leaving the conversation. Falls through to Claude
# Code itself when that cannot work.
claude() {
    if [ -n "$CLAUDECODE" ] || [ "$CCAM_WRAP" = 0 ] || [ ! -t 0 ] || ! command -v ccam >/dev/null 2>&1; then
        command claude "$@"
    else
        command ccam run --auto "$@"
    fi
}
`
	}
}

// dquote double-quotes s for embedding inside an already single-quoted
// alias value, escaping the characters that are still special inside
// double quotes.
func dquote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// escapeForSingleQuotes makes s safe to sit inside the single-quoted alias
// value the caller is building. A home directory is allowed to contain an
// apostrophe — /Users/o'brien — and one there used to close the alias early
// and leave the rest of the rc file as a dangling quote, which breaks the
// user's shell on its next start, not just ccam.
//
// POSIX shells cannot escape a quote inside single quotes at all, so the
// string is closed, an escaped quote concatenated, and the string reopened:
// '\”. Fish does support \' inside single quotes.
func escapeForSingleQuotes(shell Shell, s string) string {
	if shell == Fish {
		return strings.ReplaceAll(s, "'", `\'`)
	}
	return strings.ReplaceAll(s, "'", `'\''`)
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
