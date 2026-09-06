//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"ccam/internal/config"
)

const startupScriptName = "ccam-autostart.cmd"

type windowsService struct{ generic }

func newPlatformService(binaryPath string, port int) Service {
	return &windowsService{generic{binaryPath: binaryPath, port: port}}
}

// startupFolder returns the per-user Startup folder — anything placed
// here runs automatically at login, with no elevation/admin required.
func startupFolder() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup"), nil
}

func startupScriptPath() (string, error) {
	folder, err := startupFolder()
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, startupScriptName), nil
}

func (w *windowsService) Install(binaryPath string, port int) (string, error) {
	path, err := startupScriptPath()
	if err != nil {
		return "", err
	}
	// `start "" /min ... ` opens the process in its own minimized
	// window rather than tying it to the .cmd's own (already-hidden)
	// console, so it keeps running after the launching script exits.
	script := fmt.Sprintf("@echo off\r\nstart \"\" /min \"%s\" serve --port %d\r\n", binaryPath, port)

	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return w.installScheduledTaskFallback(binaryPath, port)
	}
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return w.installScheduledTaskFallback(binaryPath, port)
	}
	return path, nil
}

// installScheduledTaskFallback covers the rare case where the per-user
// Startup folder isn't writable (some managed/redirected profiles). A
// Scheduled Task run at logon with /RL LIMITED still needs no
// admin/elevation — it runs at the standard user's own privilege level.
func (w *windowsService) installScheduledTaskFallback(binaryPath string, port int) (string, error) {
	taskCmd := fmt.Sprintf(`"%s" serve --port %d`, binaryPath, port)
	cmd := exec.Command("schtasks", "/Create", "/F",
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/TN", "ccam",
		"/TR", taskCmd,
	)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("installing autostart (Startup folder and Scheduled Task both failed): %w", err)
	}
	return "schtasks:ccam", nil
}

func (w *windowsService) Uninstall() error {
	path, err := startupScriptPath()
	if err == nil {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
	}
	// Removing a task that doesn't exist is a no-op error we can ignore.
	_ = exec.Command("schtasks", "/Delete", "/TN", "ccam", "/F").Run()
	return w.generic.Stop()
}

func (w *windowsService) IsInstalled() (bool, error) {
	path, err := startupScriptPath()
	if err != nil {
		return false, err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return true, nil
	}
	return exec.Command("schtasks", "/Query", "/TN", "ccam").Run() == nil, nil
}
