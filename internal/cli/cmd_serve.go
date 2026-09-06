package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/httpserver"
	"ccam/internal/service"
	"ccam/internal/shellrc"
)

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", config.DefaultPort, "port to listen on")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if running, err := service.IsHTTPRunning(); err == nil && running {
		fmt.Fprintf(os.Stdout, "ccam is already running on port %s\n", currentPortOrUnknown())
		return 0
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam: resolving home directory:", err)
		return 1
	}
	accountsFile, err := config.AccountsFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	manager := accounts.NewManager(accounts.NewStore(accountsFile), accountsDir)
	syncer := shellrc.NewSyncer(home)
	srv := httpserver.New(manager, syncer, "claude")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := httpserver.Serve(ctx, srv, *port); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	return 0
}

func currentPortOrUnknown() string {
	path, err := config.PortFile()
	if err != nil {
		return "?"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "?"
	}
	return string(data)
}

// resolveBinaryPath returns the absolute, symlink-resolved path to the
// currently running ccam executable — what autostart registration and
// the manual start/stop lifecycle should point at.
func resolveBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving own executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil // fall back to the unresolved path rather than failing outright
	}
	return resolved, nil
}
