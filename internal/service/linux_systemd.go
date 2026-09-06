//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"ccam/internal/config"
)

const serviceUnitName = "ccam.service"

type linuxService struct{ generic }

func newPlatformService(binaryPath string, port int) Service {
	return &linuxService{generic{binaryPath: binaryPath, port: port}}
}

func systemdUserAvailable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	// A user manager only exists inside an active login session; outside
	// one (e.g. some CI/container contexts) `systemctl --user` fails.
	return exec.Command("systemctl", "--user", "--no-pager", "show", "-p", "Version").Run() == nil
}

func systemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", serviceUnitName), nil
}

func xdgAutostartPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", "ccam.desktop"), nil
}

func (l *linuxService) Install(binaryPath string, port int) (string, error) {
	if systemdUserAvailable() {
		return l.installSystemd(binaryPath, port)
	}
	return l.installXDGAutostart(binaryPath, port)
}

func (l *linuxService) installSystemd(binaryPath string, port int) (string, error) {
	path, err := systemdUnitPath()
	if err != nil {
		return "", err
	}
	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return "", err
	}

	unit := fmt.Sprintf(`[Unit]
Description=ccam - Claude Code Account Manager

[Service]
ExecStart=%s serve --port %d
Restart=no

[Install]
WantedBy=default.target
`, binaryPath, port)

	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return "", err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	_ = exec.Command("systemctl", "--user", "enable", serviceUnitName).Run()
	return path, nil
}

func (l *linuxService) installXDGAutostart(binaryPath string, port int) (string, error) {
	path, err := xdgAutostartPath()
	if err != nil {
		return "", err
	}
	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return "", err
	}

	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ccam
Comment=Claude Code Account Manager background service
Exec=%s serve --port %d
X-GNOME-Autostart-enabled=true
NoDisplay=true
`, binaryPath, port)

	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (l *linuxService) Uninstall() error {
	if unitPath, err := systemdUnitPath(); err == nil {
		if _, statErr := os.Stat(unitPath); statErr == nil {
			_ = exec.Command("systemctl", "--user", "disable", serviceUnitName).Run()
			_ = os.Remove(unitPath)
			_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		}
	}
	if desktopPath, err := xdgAutostartPath(); err == nil {
		if err := os.Remove(desktopPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return l.generic.Stop()
}

func (l *linuxService) IsInstalled() (bool, error) {
	if unitPath, err := systemdUnitPath(); err == nil {
		if _, statErr := os.Stat(unitPath); statErr == nil {
			return true, nil
		}
	}
	desktopPath, err := xdgAutostartPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(desktopPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
