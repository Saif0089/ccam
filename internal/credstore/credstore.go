// Package credstore reads and writes the credential store Claude Code keeps
// for one account directory — the thing that decides which login a session is
// using.
//
// It exists because of a measured fact: a RUNNING Claude Code session notices
// when its credential store changes and picks up the new login, without being
// restarted. Claude Code polls the store (by keychain generation, or by the
// credentials file's mtime) and clears its cached token when it moves. So
// switching accounts does not have to kill a session and everything running
// inside it — it can be a write to the store that session is reading.
//
// Which store a session reads is fixed when it starts, from
// CLAUDE_SECURESTORAGE_CONFIG_DIR in its environment: the path is hashed into
// a Keychain service name on macOS, and is the containing directory of
// .credentials.json elsewhere. A process's environment cannot be changed from
// outside, so ccam gives each session its own store and rewrites that one —
// never an account's own store, which every other session of that account is
// reading.
package credstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ForceFileEnvVar makes ccam keep credentials in the plaintext file store even
// where the platform has something better. Claude Code reads that file when its
// keychain item is absent — its own store is keychain-first with a plaintext
// fallback — so this stays compatible.
//
// It exists for two situations: a process with no usable keychain (a test, or a
// service running outside a login session), where the alternative is macOS
// putting a "keychain could not be found, reset it?" dialog on the user's
// screen; and a user who would rather ccam did not touch their keychain at all.
// On platforms whose store is already a file it changes nothing.
const ForceFileEnvVar = "CCAM_CREDENTIALS_FILE"

func fileOnly() bool { return os.Getenv(ForceFileEnvVar) == "1" }

// ErrNotFound means the store has no credentials — an account that has never
// been logged in, or a session store not yet seeded.
var ErrNotFound = errors.New("credstore: no credentials in this store")

// Service is the macOS Keychain service name Claude Code derives from a
// config directory. An empty dir is the user's default login, which lives in
// the bare item name; every managed account hashes to its own.
func Service(configDir string) string {
	const base = "Claude Code-credentials"
	if configDir == "" {
		return base
	}
	sum := sha256.Sum256([]byte(configDir))
	return base + "-" + hex.EncodeToString(sum[:])[:8]
}

// FilePath is where the store lives on systems without a Keychain.
func FilePath(configDir string) string {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".credentials.json"
		}
		return filepath.Join(home, ".claude", ".credentials.json")
	}
	return filepath.Join(configDir, ".credentials.json")
}

// Copy moves one account's credentials into another store, which is how both
// seeding a session and switching it are done. The source is never modified.
func Copy(fromConfigDir, toConfigDir string) error {
	data, err := Read(fromConfigDir)
	if err != nil {
		return err
	}
	return Write(toConfigDir, data)
}

// Fingerprint identifies what a store currently holds without revealing it: a
// short hash, enough to tell "unchanged", "now holds what that account holds",
// and "something rewrote this" apart. Credentials themselves never leave this
// package except through Read, and callers pass them straight to Write.
func Fingerprint(configDir string) (string, error) {
	data, err := Read(configDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12], nil
}

// Valid reports whether data is the shape Claude Code writes: one JSON object.
// A store that fails this is not credentials, whatever else it may be, and
// reading it as credentials is how a truncated write becomes a lost login.
func Valid(data []byte) bool {
	var doc map[string]json.RawMessage
	return json.Unmarshal(data, &doc) == nil
}

// HasLogin reports whether data carries an account login, as opposed to merely
// being well-formed. It guards the writes that cannot be undone: an account's
// own store is the only copy of that account's credentials, so ccam refuses to
// overwrite it with anything that is not itself a login.
func HasLogin(data []byte) bool {
	var doc struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	return doc.OAuth.AccessToken != ""
}

// Same reports whether two stores hold identical credentials. A store that
// cannot be read is not the same as anything, including another unreadable one.
func Same(aConfigDir, bConfigDir string) bool {
	a, err := Fingerprint(aConfigDir)
	if err != nil {
		return false
	}
	b, err := Fingerprint(bConfigDir)
	if err != nil {
		return false
	}
	return a == b
}
