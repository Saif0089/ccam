package cli

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/claudebin"
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

	// Only bail out if the *requested* port is the one already being
	// served; asking for a different port is a legitimate request, not
	// a duplicate start.
	if info, err := service.Running(); err == nil && info != nil && info.Port == *port {
		fmt.Fprintf(os.Stdout, "ccam is already running on http://127.0.0.1:%d\n", info.Port)
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

	capLogFile()
	// And keep capping it: every request is logged, so a service left
	// running with a page open would otherwise grow the log without
	// bound until the next restart.
	go func() {
		for range time.Tick(5 * time.Minute) {
			capLogFile()
		}
	}()

	manager := accounts.NewManager(accounts.NewStore(accountsFile), accountsDir)
	syncer := shellrc.NewSyncer(home)

	// Resolved once, here, rather than relying on PATH at spawn time:
	// started by launchd/systemd at login this process has almost no
	// PATH, so "claude" alone would not be found (see internal/claudebin).
	claude := claudebin.Resolve()
	log.Printf("using claude binary: %s", claude)
	srv := httpserver.New(manager, syncer, claude)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := httpserver.Serve(ctx, srv, *port); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	return 0
}

// maxLogBytes caps ~/.ccam/ccam.log. Every request is logged there and
// nothing ever rotated it, so a service left running with a page open
// grew it without bound — on the one file that also carries the only
// diagnostics when something goes wrong.
const maxLogBytes = 5 << 20

// capLogFile truncates the log if it has grown past maxLogBytes.
// Truncating is safe while launchd/systemd hold it open, because they
// opened it O_APPEND and will simply continue at the new end.
func capLogFile() {
	path, err := config.LogFile()
	if err != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxLogBytes {
		return
	}
	_ = os.Truncate(path, 0)
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
