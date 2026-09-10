//go:build !darwin

package credstore

import (
	"fmt"
	"os"
	"path/filepath"
)

// Everywhere except macOS the store Claude Code reads is a file,
// <configDir>/.credentials.json, and it watches that file's mtime — which is
// what makes an in-place switch work by writing it.
//
// Windows can instead keep credentials in the Credential Manager when one is
// available. ccam does not write that; the caller checks after seeding that the
// store it wrote is the one the session actually reads, and falls back to
// relaunching the session when it is not.

// Read returns the credentials in the store for configDir.
func Read(configDir string) ([]byte, error) {
	data, err := os.ReadFile(FilePath(configDir))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if len(data) == 0 {
		return nil, ErrNotFound
	}
	return data, nil
}

// Write replaces the credentials in the store for configDir. It writes a temp
// file and renames it into place: a session reading the store concurrently sees
// either the old credentials or the new ones, never half of either.
func Write(configDir string, data []byte) error {
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Delete removes the store. A store that is not there is not an error.
func Delete(configDir string) error {
	if err := os.Remove(FilePath(configDir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
