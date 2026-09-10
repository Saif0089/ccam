package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/credstore"
	"ccam/internal/switching"
)

// mirrorInterval is how often the supervisor checks whether Claude Code has
// rewritten the session's credential store — which it does when it refreshes an
// access token. Rare (tokens last hours), so this is cheap and slow on purpose.
var mirrorInterval = 30 * time.Second

// sessionCreds owns one session's credential store: a store of its own, seeded
// from whichever account the session runs as.
//
// It exists so that switching an account does not have to restart anything. A
// running Claude Code session re-reads its credential store when it changes,
// so writing another account's credentials into THIS session's store moves this
// session — and only this session — to that account. Rewriting the account's
// own store would have moved every terminal reading it, and destroyed that
// account's stored login in the process.
//
// The mirror is the other half of the bargain: because the session now refreshes
// tokens into its private store, those refreshed tokens have to be copied back
// to the account, or the account's own store would slowly go stale.
type sessionCreds struct {
	mu       sync.Mutex
	storeDir string           // this session's store; "" when unavailable
	acct     accounts.Account // the account the session is currently running as
	written  string           // fingerprint of what ccam last put there
	ledger   string           // the usage monitor's ledger, or ""
	dirsMade []string         // cleaned up on exit
}

// newSessionCreds seeds a private store for the session from acct and returns
// it ready to use. It returns nil when a private store cannot be used — no
// credentials to copy, or a platform where ccam cannot write the store Claude
// Code reads. The caller then runs the account's own store and switches by
// relaunching, exactly as before.
func newSessionCreds(sessionsDir string, acct accounts.Account, ledger string) *sessionCreds {
	storeDir := filepath.Join(sessionsDir, fmt.Sprintf("s-%d", os.Getpid()))
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return nil
	}
	if err := credstore.Copy(acct.ConfigDir, storeDir); err != nil {
		os.RemoveAll(storeDir)
		return nil
	}
	fp, err := credstore.Fingerprint(storeDir)
	if err != nil {
		credstore.Delete(storeDir)
		os.RemoveAll(storeDir)
		return nil
	}
	return &sessionCreds{storeDir: storeDir, acct: acct, written: fp, ledger: ledger, dirsMade: []string{storeDir}}
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
// Returns false if it could not be done, in which case the caller relaunches.
func (s *sessionCreds) switchTo(next accounts.Account, sessionID string) bool {
	if s == nil || s.storeDir == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Anything the session refreshed under the OLD account belongs to the old
	// account, so mirror before overwriting.
	s.mirrorLocked()

	if err := credstore.Copy(next.ConfigDir, s.storeDir); err != nil {
		fmt.Fprintln(os.Stderr, "ccam: could not switch credentials in place:", err)
		return false
	}
	// Read back before believing it. A write can land somewhere Claude Code
	// will not look: on Windows it prefers the Credential Manager when one is
	// available, and on macOS it migrates a session's credentials from the
	// plaintext file into the keychain the first time it refreshes a token, so
	// a file written afterwards is ignored. Neither announces itself. If what
	// the store now reads back is not the account we asked for, the switch did
	// not happen and the caller must relaunch instead of reporting a switch
	// that never took.
	if !credstore.Same(next.ConfigDir, s.storeDir) {
		fmt.Fprintln(os.Stderr, "ccam: the credential store did not take the new account; relaunching instead")
		return false
	}
	fp, err := credstore.Fingerprint(s.storeDir)
	if err != nil {
		return false
	}
	s.acct, s.written = next, fp
	if err := switching.AppendOwnership(s.ledger, sessionID, next.ConfigDir); err != nil {
		// Attribution is worth a warning, not a failed switch.
		fmt.Fprintln(os.Stderr, "ccam: could not record the switch for the usage monitor:", err)
	}
	return true
}

// mirror copies a token Claude Code refreshed in the session's private store
// back to the account it belongs to, so the account's own store does not go
// stale while a long session runs.
func (s *sessionCreds) mirror() {
	if s == nil || s.storeDir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mirrorLocked()
}

func (s *sessionCreds) mirrorLocked() {
	fp, err := credstore.Fingerprint(s.storeDir)
	if err != nil || fp == s.written {
		return // unreadable, or nothing has changed since ccam wrote it
	}
	data, err := credstore.Read(s.storeDir)
	if err != nil {
		return
	}
	if err := credstore.Write(s.acct.ConfigDir, data); err != nil {
		return
	}
	s.written = fp
}

// close mirrors one last time and removes the session's store. A store left
// behind would be a stale copy of a login sitting in the keychain.
func (s *sessionCreds) close() {
	if s == nil || s.storeDir == "" {
		return
	}
	s.mirror()
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
