package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ccam/internal/config"
)

// logLive records something that happened while a Claude Code session was
// running.
//
// It exists because the obvious destination is the wrong one. The supervisor
// launches Claude Code with stdio inherited, so Claude Code owns the terminal;
// anything ccam prints from that point lands inside the screen the TUI is
// drawing and leaves it corrupted until a full repaint. Whatever the user needs
// to see goes back through the hook, which Claude Code renders itself.
// Everything else — a mirror that declined to copy, an attribution line that
// could not be written — belongs in a file that can be read afterwards.
//
// Failing to log is never worth interrupting a session over, so every error
// here is dropped.
func logLive(format string, args ...any) {
	accountsDir, err := config.AccountsDir()
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(accountsDir), "session.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s ccam: %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
