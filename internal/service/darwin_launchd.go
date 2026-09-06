//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"ccam/internal/config"
)

const launchAgentLabel = "com.ccam.agent"

type darwinService struct{ generic }

func newPlatformService(binaryPath string, port int) Service {
	return &darwinService{generic{binaryPath: binaryPath, port: port}}
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}

func (d *darwinService) Install(binaryPath string, port int) (string, error) {
	path, err := launchAgentPath()
	if err != nil {
		return "", err
	}
	logPath, err := config.LogFile()
	if err != nil {
		return "", err
	}
	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := config.EnsureDir(filepath.Dir(logPath)); err != nil {
		return "", err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>serve</string>
		<string>--port</string>
		<string>%d</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, launchAgentLabel, binaryPath, port, logPath, logPath)

	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return "", err
	}

	target, err := guiTarget()
	if err != nil {
		return path, nil // plist is written; loading it can be retried by Start()
	}
	// Loading twice is harmless; launchctl reports an error we ignore.
	_ = exec.Command("launchctl", "bootstrap", target, path).Run()
	return path, nil
}

func (d *darwinService) Uninstall() error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	if target, err := guiTarget(); err == nil {
		_ = exec.Command("launchctl", "bootout", target+"/"+launchAgentLabel).Run()
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return d.generic.Stop()
}

func (d *darwinService) IsInstalled() (bool, error) {
	path, err := launchAgentPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func guiTarget() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return "gui/" + u.Uid, nil
}
