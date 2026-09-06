//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	// Whichever mechanism we're about to use, clear the other one:
	// installing once from a desktop session (systemd) and later over
	// SSH without a user bus (XDG autostart) would otherwise leave both
	// registered, and at the next login they'd both start a ccam, one
	// of which loses the port and dies with only a log line to show it.
	l.removeSystemdUnit()
	l.removeXDGAutostart()

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

	// ExecStart= splits on whitespace, so the binary path is quoted for
	// home directories containing spaces. Environment= carries the
	// installing shell's PATH so `claude` (and the node it needs) can
	// be found, and WorkingDirectory keeps claude out of "/".
	unit := fmt.Sprintf(`[Unit]
Description=ccam - Claude Code Account Manager

[Service]
ExecStart="%s" serve --port %d
WorkingDirectory=%s
Environment="PATH=%s"
Restart=no

[Install]
WantedBy=default.target
`, binaryPath, port, serviceWorkingDir(), servicePATH())

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

	// Exec= splits on whitespace too, and the Desktop Entry spec quotes
	// with double quotes and escapes with backslashes.
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ccam
Comment=Claude Code Account Manager background service
Exec="%s" serve --port %d
Path=%s
X-GNOME-Autostart-enabled=true
NoDisplay=true
`, desktopEntryEscape(binaryPath), port, serviceWorkingDir())

	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (l *linuxService) Uninstall() error {
	l.removeSystemdUnit()
	l.removeXDGAutostart()
	return l.generic.Stop()
}

// removeSystemdUnit disables and deletes the user unit if present.
func (l *linuxService) removeSystemdUnit() {
	unitPath, err := systemdUnitPath()
	if err != nil {
		return
	}
	if _, statErr := os.Stat(unitPath); statErr != nil {
		return
	}

	// `systemctl --user disable` is what removes the enablement
	// symlink, but it fails when there's no user bus (an SSH session,
	// a container). Remove the symlink directly as well, or a broken
	// link is left in default.target.wants for systemd to complain
	// about at every subsequent login.
	_ = exec.Command("systemctl", "--user", "disable", serviceUnitName).Run()
	_ = os.Remove(filepath.Join(filepath.Dir(unitPath), "default.target.wants", serviceUnitName))
	_ = os.Remove(unitPath)
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
}

func (l *linuxService) removeXDGAutostart() {
	if desktopPath, err := xdgAutostartPath(); err == nil {
		_ = os.Remove(desktopPath)
	}
}

// desktopEntryEscape escapes a value for a Desktop Entry Exec= field,
// where backslash and double quote are both special.
func desktopEntryEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
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
