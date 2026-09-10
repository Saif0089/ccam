package cli

import (
	"os"
	"path/filepath"
	"sync"

	"ccam/internal/accounts"
	"ccam/internal/credstore"
	"ccam/internal/switching"
)

// sessionCreds owns one session's credential store: a copy of an account's
// login that belongs to this session and nothing else.
//
// It is what makes switching accounts free of restarts. A running Claude Code
// session re-reads its credential store when it changes, so writing another
// account's credentials into THIS store moves THIS session — and only this
// session — to that account. Terminal or editor, it is the same store in the
// same place; nothing about it is specific to how the session was started.
//
// One rule keeps it safe, and it is worth stating plainly: an account's own
// store is READ-ONLY here. ccam copies out of it and never back into it.
//
// That rule is not caution, it is history. Every credential loss in this project
// came from writing an account's store with something that was not that
// account's login — first a truncated blob, then, through a "mirror" that
// copied a session's store back to whichever account the session was bookkept
// against, the credentials of the account it was switching TO. A failed switch
// left the store holding the new account's login while the old account was
// still recorded, and the next mirror wrote one account's login over another's.
// Three stores on this machine ended up byte-for-byte identical that way.
//
// The mirror is gone. The cost is that a token Claude Code refreshes inside a
// session is not copied back, so an account's own store keeps the refresh token
// it already had — which is exactly what a refresh token is for. The next
// session of that account refreshes from it again.
type sessionCreds struct {
	mu       sync.Mutex
	storeDir string           // this session's store; "" when unavailable
	acct     accounts.Account // the account the session is currently running as
	ledger   string           // the usage monitor's ledger, or ""
	dirsMade []string         // cleaned up on exit
}

// newSessionCreds seeds a private store for the session from acct and returns
// it ready to use. It returns nil when a private store cannot be used — no
// credentials to copy, or a platform where ccam cannot write the store Claude
// Code reads. The caller then runs the account's own store and switches by
// relaunching, exactly as before.
func newSessionCreds(sessionsDir string, acct accounts.Account, ledger string) *sessionCreds {
	storeDir := filepath.Join(sessionsDir, "s-"+itoa(os.Getpid()))
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return nil
	}
	if err := credstore.Copy(acct.ConfigDir, storeDir); err != nil {
		os.RemoveAll(storeDir)
		return nil
	}
	// Read the seed back before running on it. A store that does not hold what
	// the account holds is one the session would spend its life reading.
	if !credstore.Same(acct.ConfigDir, storeDir) {
		credstore.Delete(storeDir)
		os.RemoveAll(storeDir)
		return nil
	}
	return &sessionCreds{storeDir: storeDir, acct: acct, ledger: ledger, dirsMade: []string{storeDir}}
}

// dir is the directory to point CLAUDE_SECURESTORAGE_CONFIG_DIR at.
func (s *sessionCreds) dir() string {
	if s == nil {
		return ""
	}
	return s.storeDir
}

// switchTo moves the running session onto another account by rewriting its
// store, and records the change in the usage monitor's ledger so the tokens
// spent from here on are attributed to the account that is now paying for them.
//
// It returns false, and why, if it could not be done — the caller relaunches
// instead. Nothing here prints: Claude Code owns the terminal while this runs,
// so the reason is handed back to be reported through the hook rather than
// written over the screen the TUI is painting.
func (s *sessionCreds) switchTo(next accounts.Account, sessionID string) (bool, string) {
	if s == nil || s.storeDir == "" {
		return false, "this session has no credential store of its own"
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := credstore.Copy(next.ConfigDir, s.storeDir); err != nil {
		return false, "the credentials could not be written: " + err.Error()
	}
	// Read back before believing it. A write can land somewhere Claude Code
	// will not look: on Windows it prefers the Credential Manager when one is
	// available, and on macOS it migrates a session's credentials from the
	// plaintext file into the keychain the first time it refreshes a token, so
	// a file written afterwards is ignored. Neither announces itself.
	if !credstore.Same(next.ConfigDir, s.storeDir) {
		return false, "the credential store did not take the new account"
	}
	s.acct = next
	if err := switching.AppendOwnership(s.ledger, sessionID, next.ConfigDir); err != nil {
		// Attribution is worth a note, not a failed switch — and not a write to
		// a terminal Claude Code is drawing on.
		logLive("could not record the switch for the usage monitor: %v", err)
	}
	return true, ""
}

// close removes the session's store. A store left behind would be a stale copy
// of a login sitting in the keychain for ever.
func (s *sessionCreds) close() {
	if s == nil || s.storeDir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	credstore.Delete(s.storeDir)
	for _, d := range s.dirsMade {
		os.RemoveAll(d)
	}
	s.storeDir = ""
}

// current is the account the session is running as right now.
func (s *sessionCreds) current() accounts.Account {
	if s == nil {
		return accounts.Account{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acct
}

// itoa keeps the store directory name free of fmt just for one integer.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
