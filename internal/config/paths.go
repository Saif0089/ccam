// Package config resolves the per-user, per-OS filesystem locations ccam
// uses. Everything lives under the user's home directory — no path here
// ever requires elevated permissions to create or write.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// HomeDir returns the ccam base directory: ~/.ccam on every OS. Using a
// single dotdir (rather than OS-specific "proper" locations) keeps the
// install/uninstall and e2e-test logic identical across platforms.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ccam"), nil
}

// AccountsDir returns ~/.ccam/accounts, the parent of every per-account
// CLAUDE_CONFIG_DIR.
func AccountsDir() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "accounts"), nil
}

// AccountsFile returns the path to the account metadata store.
func AccountsFile() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "accounts.json"), nil
}

// LogFile returns the path ccam's background service writes its own
// stdout/stderr to, so install issues are debuggable without a terminal.
func LogFile() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ccam.log"), nil
}

// PortFile returns the path ccam writes its listening port to, so the CLI
// (and the installer's health check) can find a running instance without
// hardcoding a port.
func PortFile() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "port"), nil
}

// InstallDir returns the default per-user directory the install script
// copies the ccam binary into. Never a system location (no /usr/local, no
// Program Files) — nothing under here ever needs admin/root to write.
func InstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "ccam", "bin"), nil
	default:
		return filepath.Join(home, ".local", "bin"), nil
	}
}

// EnsureDir creates dir (and parents) if missing, with permissions that
// only the owning user can read/write.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}
