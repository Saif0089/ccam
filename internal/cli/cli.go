// Package cli wires ccam's subcommands. It's the only package that
// knows about os.Args, exit codes, and signal handling — every
// subcommand is a thin adapter onto internal/httpserver,
// internal/service, internal/accounts, and internal/shellrc.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ccam/internal/buildinfo"
	"ccam/internal/switching"
)

// Run executes the subcommand named by args[0] and returns a process
// exit code.
func Run(args []string) int {
	if len(args) == 0 {
		// A bare `ccam` is someone asking what this is. Answer with the full
		// help on stdout, not a terse error — this is the front door.
		printUsage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "install":
		return cmdInstall(args[1:])
	case "uninstall":
		return cmdUninstall(args[1:])
	case "start":
		return cmdStart(args[1:])
	case "stop":
		return cmdStop(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "hook":
		return cmdHook(args[1:])
	case "use":
		return cmdUse(args[1:])
	case "shared":
		return cmdShared(args[1:])
	case "join":
		return cmdJoin(args[1:])
	case "panel":
		return cmdPanel(args[1:])
	case "prune":
		return cmdPrune(args[1:])
	case "exec":
		return cmdExec(args[1:])
	case "editor", "vscode", "code":
		return cmdEditor(args[1:])
	case "version", "--version", "-v":
		fmt.Println(buildinfo.Version)
		return 0
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return 0
	default:
		// Bare `ccam <account>` is shorthand for `ccam run <account>`: it
		// starts a switchable session for that account. Only a name that
		// actually resolves to an account is treated this way; anything else
		// is an unknown command.
		if list, err := loadAccounts(); err == nil {
			if _, ok := switching.ResolveAccount(list, args[0]); ok {
				return cmdRun(args)
			}
		}
		// An editor calls its wrapper as `<wrapper> <real-claude-binary>
		// [args...]`, with no subcommand to put `exec` in front of — the
		// setting is one executable path and nothing else. So a first argument
		// that is an executable file, rather than a word, IS that invocation.
		// Without this the Claude Code extension got ccam's usage text on
		// stderr and exit 1, which it reports as "Claude Code process exited
		// with code 1" and no chat at all.
		if isExecutablePath(args[0]) {
			return cmdExec(args)
		}
		// Almost everything that reaches here is a mistyped or removed ACCOUNT,
		// not a mistyped subcommand — `ccam <account>` is the command people
		// type all day. Answering it with "unknown command" and the full usage
		// text buried the one fact that helps, so say which accounts exist and
		// leave the usage to `ccam help`.
		if list, err := loadAccounts(); err == nil {
			fmt.Fprintf(os.Stderr, "ccam: no account called %q. Accounts on this machine: %s.\n", args[0], accountNames(list))
			fmt.Fprintln(os.Stderr, "      Add one at the ccam web UI, or run `ccam help` for the list of commands.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "ccam: unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 1
	}
}

func printUsage(w *os.File) {
	// The shell shortcut for an account only exists where ccam writes shell rc
	// aliases; on Windows it usually does not, so the portable `ccam <name>`
	// form is named first and the shortcut is shown as the extra it is.
	shortcut := "or the `claude-<name>` shortcut your shell sets up"
	if runtime.GOOS == "windows" {
		shortcut = "the `claude-<name>` shortcut is not set up on Windows"
	}
	fmt.Fprint(w, `ccam — run and share Claude Code accounts

EVERYDAY
  ccam                       Show this help
  ccam <name> [args...]      Run Claude as an account (`+shortcut+`)
  ccam use <gateway> <key>   Run Claude on a shared account, through a gateway
  ccam status                Is the ccam service running, and on what URL

ACCOUNTS live on the web page ccam opens — add, connect, and remove them there:
  ccam install [--port N]    Start ccam at login and open the page (do this once)
  ccam start | stop          Start or stop the ccam service now
  ccam editor [name]         Point VS Code / Cursor at an account (blank: show which)
  ccam prune [--yes] [id...] Reclaim disk from old accounts (previews unless --yes)

SHARING one account with other people (needs a panel + gateway):
  ccam join <invite-link>    Connect this machine to a panel from an invite link
  ccam panel <command>       Run or manage the panel — see `+"`ccam panel help`"+`

OTHER
  ccam uninstall             Remove the service, autostart, and shell aliases
  ccam serve [--port N]      Run the server in the foreground (what the service runs)
  ccam version               Print the version

The web page is where accounts are managed; the commands above are the shortcuts.
`)
}

// isExecutablePath reports whether arg names a program on disk rather than a
// ccam subcommand or an account. Subcommands are single words and account names
// are slugs, so neither ever contains a path separator; the file also has to
// exist and be executable, which keeps a stray argument from being run.
func isExecutablePath(arg string) bool {
	if arg == "" || !strings.ContainsRune(arg, filepath.Separator) {
		return false
	}
	info, err := os.Stat(arg)
	if err != nil || info.IsDir() {
		return false
	}
	// Windows has no executable bit; there the extension is the only thing
	// passing a path, and the extension only passes its own binary.
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}
