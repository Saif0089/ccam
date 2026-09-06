package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ccam/internal/config"
)

func pidFilePath() (string, error) {
	base, err := config.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ccam.pid"), nil
}

func writePID(pid int) error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o600)
}

// readPID returns 0, nil if no pidfile exists.
func readPID() (int, error) {
	path, err := pidFilePath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, nil // corrupt pidfile is treated as "no process", not fatal
	}
	return pid, nil
}

func removePIDFile() error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
