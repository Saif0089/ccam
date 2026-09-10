package switching

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LedgerFile is the session-ownership ledger the Claude usage monitor keeps in
// the user's home directory. Its hook appends a line when a session starts,
// recording which account directory that session inherited; the monitor's
// scanner then attributes each transcript by it.
const LedgerFile = ".claude-usage-monitor-sessions.tsv"

// LedgerPath is the ledger's location, or "" if the home directory is unknown.
func LedgerPath(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, LedgerFile)
}

// AppendOwnership records that a session now belongs to an account, in the
// monitor's own ledger format: session id, owner directory, epoch seconds.
//
// ccam has to write this itself because an in-place switch has no restart, and
// therefore no SessionStart hook: the only record of the change would otherwise
// be ccam's own memory, and the session's tokens would keep being attributed to
// the account it started as. The monitor resolves ownership by interval — the
// last entry at or before a line's timestamp — so appending is enough, and the
// account's earlier work stays with the account that did it.
//
// ownerDir is the account's own directory, and "" for the default login, which
// is exactly what the hook would have written. Nothing is written when the
// ledger does not exist: the monitor is not installed, and creating the file
// would fabricate a cutover point that credits everything before it to nobody.
func AppendOwnership(ledgerPath, sessionID, ownerDir string) error {
	if ledgerPath == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if strings.ContainsAny(sessionID, "\t\n") {
		return fmt.Errorf("refusing to write a session id containing a tab or newline")
	}
	if _, err := os.Stat(ledgerPath); err != nil {
		return nil // no monitor on this machine
	}
	owner := ownerDir
	if owner != "" {
		if resolved, err := filepath.EvalSymlinks(owner); err == nil {
			owner = resolved // the hook writes the realpath; match it
		}
	}
	f, err := os.OpenFile(ledgerPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\t%s\t%f\n", sessionID, owner, float64(time.Now().UnixNano())/1e9)
	return err
}
