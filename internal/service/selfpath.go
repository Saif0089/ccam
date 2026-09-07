package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// SelfPath is the absolute, symlink-resolved path of the running
// executable — the file an update replaces and Respawn then runs.
func SelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving own executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil
	}
	return resolved, nil
}
