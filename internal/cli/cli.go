// Package cli wires ccam's subcommands. It's the only package that
// knows about os.Args, exit codes, and signal handling — every
// subcommand is a thin adapter onto internal/httpserver,
// internal/service, internal/accounts, and internal/shellrc.
package cli

import (
	"fmt"
	"os"

	"ccam/internal/buildinfo"
	"ccam/internal/switching"
)

// Run executes the subcommand named by args[0] and returns a process
// exit code.
func Run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 1
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
	case "prune":
		return cmdPrune(args[1:])
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
		fmt.Fprintf(os.Stderr, "ccam: unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 1
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `ccam - Claude Code Account Manager

Usage:
  ccam install [--port N]    Register the background service to start at login, and start it now
  ccam uninstall             Stop the service, remove autostart registration and shell aliases
  ccam start [--port N]      Start the background service now (without waiting for login)
  ccam stop                  Stop the background service
  ccam status                Report whether the service is running, and its URL
  ccam serve [--port N]      Run the server in the foreground (this is what the service actually runs)
  ccam <account> [args...]   Start a switchable Claude session for an account; inside it, type
                             `+"`ccam <name>`"+` to switch accounts in place, keeping the conversation
  ccam editor [account]      Point VS Code (and Cursor, VSCodium, ...) at an account;
                             with no account, says which one each editor is on
  ccam prune [--yes] [id...] Reclaim disk from migrated accounts (previews unless --yes)
  ccam version                Print the version

Once running, open the printed URL in a browser to manage accounts.
`)
}
