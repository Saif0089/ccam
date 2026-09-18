package config

import (
	"os"
	"path/filepath"
)

// The revocation marker is how the panel, running in the background service,
// tells a live session — a separate process — that its account has been taken
// back. It is a file per account under ~/.clawdh/revoked/. The session supervisor
// already polls for account switches every 150ms; it checks for this marker on
// the same tick, so a revoked session stops within that interval rather than
// limping on its in-memory token until the next token refresh.

func revokedDir() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "revoked"), nil
}

func revokedMarker(accountID string) (string, error) {
	dir, err := revokedDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, accountID), nil
}

// MarkRevoked records that an account was taken back, so any live session on it
// stops. Best-effort: a session that cannot be told still loses its login.
func MarkRevoked(accountID string) error {
	path, err := revokedMarker(accountID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte{}, 0o600)
}

// IsRevoked reports whether an account has an outstanding revocation. The
// supervisor calls this every poll tick, so it is a single cheap stat.
func IsRevoked(accountID string) bool {
	path, err := revokedMarker(accountID)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// ClearRevoked removes an account's marker. The panel clears it when the same
// account is granted again, so a fresh session on it is not stopped by a stale
// marker from a previous revocation.
func ClearRevoked(accountID string) error {
	path, err := revokedMarker(accountID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
