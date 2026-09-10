package credstore

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
)

// On macOS the store is a Keychain generic password. ccam drives /usr/bin/security
// rather than the Keychain API for the same reason it does when reading usage:
// Keychain access is granted per executable, and the item belongs to Claude
// Code — going through the tool a person would use by hand inherits that
// access instead of prompting under ccam's own unfamiliar name.

// account is the item's account attribute. Claude Code writes the current
// username, and an item is identified by service AND account, so ccam has to
// use the same one or it would create a second item the CLI never reads.
func account() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}

// Read returns the credentials in the store for configDir. The keychain is
// consulted first and the plaintext file second, which is the order Claude Code
// itself reads them in.
//
// Reading deliberately ignores ForceFileEnvVar. That setting is about where
// ccam PUTS credentials; an account's own login is wherever Claude Code left
// it, which on macOS is the keychain. Honouring it here meant ccam could not
// read the account it was asked to copy from, so seeding a session store failed
// and every switch quietly fell back to relaunching.
func Read(configDir string) ([]byte, error) {
	if data, err := readKeychain(configDir); err == nil {
		return data, nil
	}
	return readFile(configDir)
}

func readFile(configDir string) ([]byte, error) {
	data, err := os.ReadFile(FilePath(configDir))
	if err != nil || len(data) == 0 {
		return nil, ErrNotFound
	}
	return data, nil
}

func writeFile(configDir string, data []byte) error {
	path := FilePath(configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ccam-cred-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func readKeychain(configDir string) ([]byte, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password",
		"-a", account(), "-s", Service(configDir), "-w").Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	data := []byte(strings.TrimSpace(string(out)))
	if len(data) == 0 {
		return nil, ErrNotFound
	}
	return data, nil
}

// loginKeychain is the keychain to write into, resolved once.
//
// Naming it matters. Without one, `security` writes to the *default* keychain,
// which it can only resolve inside a normal login session — from a launchd
// service, an ssh session, or a test process it instead puts a dialog on the
// user's screen saying the keychain could not be found and offering to reset it
// to the default. Cancelling that is the right answer and leaves ccam broken;
// accepting it rewrites the user's keychain search list. Neither should ever be
// on offer, so ccam says which keychain it means.
var loginKeychain = sync.OnceValue(func() string {
	if out, err := exec.Command("/usr/bin/security", "login-keychain", "-d", "user").Output(); err == nil {
		if p := strings.Trim(strings.TrimSpace(string(out)), `"`); p != "" {
			if _, statErr := os.Stat(p); statErr == nil {
				return p
			}
		}
	}
	// The OS user's own home, not $HOME: the login keychain belongs to the
	// account, and the service ccam runs as (or a test) may have $HOME pointed
	// somewhere else entirely.
	homes := []string{}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		homes = append(homes, u.HomeDir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		homes = append(homes, home)
	}
	for _, home := range homes {
		p := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "" // fall back to the default keychain and hope we are in a session
})

// Write replaces the credentials in the store for configDir. A keychain that
// cannot be written falls back to the plaintext file rather than failing —
// exactly what Claude Code does with the same pair of stores, and what keeps a
// switch working in a context where the keychain is unavailable.
func Write(configDir string, data []byte) error {
	if !fileOnly() {
		if err := writeKeychain(configDir, data); err == nil {
			return nil
		}
	}
	return writeFile(configDir, data)
}

// writeKeychain replaces the credentials in the keychain item, creating it
// if it is not there.
//
// The payload goes in hex via -X, and the whole command is fed to `security -i`
// on stdin, so the credential never appears in this process's argv where every
// other process on the machine could read it out of ps. This is how Claude Code
// writes the same item.
//
// -T grants the item to /usr/bin/security itself, which is the tool BOTH ccam
// and Claude Code read it with. Without it, an item ccam created prompts for
// authorization the first time the other one touches it, which for a switch
// means a dialog in the middle of a conversation.
func writeKeychain(configDir string, data []byte) error {
	cmd := fmt.Sprintf("add-generic-password -U -a %q -s %q -X %q -T /usr/bin/security",
		account(), Service(configDir), hex.EncodeToString(data))
	if kc := loginKeychain(); kc != "" {
		cmd += fmt.Sprintf(" %q", kc)
	}
	c := exec.Command("/usr/bin/security", "-i")
	c.Stdin = strings.NewReader(cmd + "\n")
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("writing the credential store: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Delete removes the store, in both places it could be.
func Delete(configDir string) error {
	if !fileOnly() {
		exec.Command("/usr/bin/security", "delete-generic-password",
			"-a", account(), "-s", Service(configDir)).Run()
	}
	if err := os.Remove(FilePath(configDir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := Read(configDir); err == nil {
		return fmt.Errorf("the credential store is still readable after removing it")
	}
	return nil
}

// KeychainBacked reports whether this store is the keychain rather than the
// file. It matters to callers because the two behave differently under a
// switch: Claude Code re-reads the file on every request, but caches keychain
// reads for thirty seconds, so a keychain-backed switch can take that long to
// be visible.
func KeychainBacked(configDir string) bool {
	if configDir == "" {
		return true
	}
	_, err := readKeychain(configDir)
	return err == nil
}
