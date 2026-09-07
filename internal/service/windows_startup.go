//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	// Clear a Scheduled Task left by an earlier install that had to
	// fall back to one; otherwise both it and the Startup script fire
	// at logon and one of the two ccams loses the port.
	_ = exec.Command("schtasks", "/Delete", "/TN", "ccam", "/F").Run()

	script := autostartScript(binaryPath, port)

	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return w.installScheduledTaskFallback(binaryPath, port)
	}
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return w.installScheduledTaskFallback(binaryPath, port)
	}
	return path, nil
}

// autostartScript is the batch file that starts ccam at logon.
//
// `start "" /min ...` opens the process in its own minimized window
// rather than tying it to the .cmd's own (already hidden) console, so
// it keeps running after the launching script exits. PATH is set first
// so ccam can find `claude` (and the node it needs), which a
// logon-launched process would not otherwise have — and cmd.exe expands
// %VAR% as it parses the line, so a PATH containing a literal
// %SOMETHING% (common on Windows) would be silently mangled unless the
// percent signs are doubled.
func autostartScript(binaryPath string, port int) string {
	// Every interpolated value goes through batchEscape, not just PATH:
	// the home directory and the install path both sit under
	// C:\Users\<account name>, and % is a legal character in a Windows
	// account name. Left bare, cmd.exe would eat it at parse time and
	// silently start ccam from the wrong directory — or not at all.
	return fmt.Sprintf("@echo off\r\nset \"PATH=%s\"\r\ncd /d \"%s\"\r\nstart \"\" /min \"%s\" serve --port %d\r\n",
		batchEscape(servicePATH()), batchEscape(serviceWorkingDir()), batchEscape(binaryPath), port)
}

// installScheduledTaskFallback covers the rare case where the per-user
// Startup folder isn't writable (some managed/redirected profiles). A
// Scheduled Task run at logon with /RL LIMITED still needs no
// admin/elevation — it runs at the standard user's own privilege level.
func (w *windowsService) installScheduledTaskFallback(binaryPath string, port int) (string, error) {
	// The task runs the same script the Startup folder would, rather
	// than an inline command line. Inlining it put the whole PATH into
	// /TR, which schtasks truncates at 261 characters — so on the very
	// managed profiles this fallback exists for, registration failed.
	// It also needs the same PATH and working directory, or this path
	// reproduces the bug where ccam cannot find claude at all.
	scriptPath, err := fallbackScriptPath()
	if err != nil {
		return "", err
	}
	if err := config.EnsureDir(filepath.Dir(scriptPath)); err != nil {
		return "", fmt.Errorf("installing autostart: %w", err)
	}
	if err := os.WriteFile(scriptPath, []byte(autostartScript(binaryPath, port)), 0o644); err != nil {
		return "", fmt.Errorf("installing autostart: %w", err)
	}

	cmd := exec.Command("schtasks", "/Create", "/F",
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/TN", "ccam",
		"/TR", `"`+scriptPath+`"`,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("installing autostart (Startup folder and Scheduled Task both failed): %w: %s",
			err, strings.TrimSpace(string(out)))
	}
	return "schtasks:ccam", nil
}

// fallbackScriptPath is where the Scheduled Task's script lives, in
// ccam's own per-user directory rather than the Startup folder that was
// unwritable.
func fallbackScriptPath() (string, error) {
	base, err := config.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, startupScriptName), nil
}

// batchEscape makes a value safe to embed in a .cmd line: cmd.exe
// expands %VAR% at parse time, and a quote would terminate the set
// statement early.
func batchEscape(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	return strings.ReplaceAll(s, "%", "%%")
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
	if scriptPath, err := fallbackScriptPath(); err == nil {
		_ = os.Remove(scriptPath)
	}
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
