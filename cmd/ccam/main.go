// Command ccam is the Claude Code Account Manager: a local web UI and
// background service for managing multiple `claude` CLI logins on one
// machine. See internal/cli for its subcommands.
package main

import (
	"os"

	"ccam/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
