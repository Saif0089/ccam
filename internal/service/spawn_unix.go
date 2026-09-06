//go:build !windows

package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"ccam/internal/config"
)

// spawnDetached starts binaryPath as a new session leader (so it
// survives the parent exiting) with its output redirected to ccam's log
// file, and returns its PID.
func spawnDetached(binaryPath string, args []string) (int, error) {
	logPath, err := config.LogFile()
	if err != nil {
		return 0, err
	}
	if err := config.EnsureDir(filepath.Dir(logPath)); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	cmd := exec.Command(binaryPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// Let the child run independently; we only needed its PID.
	go cmd.Wait()
	return cmd.Process.Pid, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
