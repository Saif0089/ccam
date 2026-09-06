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

	// launchd starts login agents with a bare PATH (roughly
	// /usr/bin:/bin:/usr/sbin:/sbin), but ccam has to run `claude`,
	// which normally lives under the user's home and is itself a Node
	// program needing more of the user's PATH. Bake in the PATH of the
	// shell that ran `ccam install`, which is exactly the environment
	// where the user's `claude` works.
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
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>%s</string>
	</dict>
	<key>WorkingDirectory</key>
	<string>%s</string>
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
`, launchAgentLabel, xmlEscape(binaryPath), port, xmlEscape(servicePATH()), xmlEscape(serviceWorkingDir()), xmlEscape(logPath), xmlEscape(logPath))

	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return "", err
	}

	// If the user previously turned this off in System Settings →
	// General → Login Items, launchd remembers that as a persistent
	// disabled flag, and rewriting the identical plist would leave it
	// off forever with nothing to show why. `enable` only clears that
	// flag — unlike `bootstrap` it does not start anything, so it
	// can't reintroduce the race described below.
	if target, err := guiTarget(); err == nil {
		_ = exec.Command("launchctl", "enable", target+"/"+launchAgentLabel).Run()
	}

	// Deliberately not `launchctl bootstrap`-ing it here: with
	// RunAtLoad=true, bootstrapping loads *and* immediately starts it,
	// racing with the Start() call every caller (ccam install, and our
	// own tests) makes right after Install() — whichever of the two
	// wins the race to bind the port leaves the other logging a
	// harmless-looking "address already in use" error, and on a loaded
	// CI runner the loser can occasionally be the one whose bind
	// mattered. macOS already loads ~/Library/LaunchAgents/*.plist at
	// the next real login on its own, so writing the file is enough
	// for "start at login"; Start() is the sole "start it right now"
	// path, matching generic.go's design (see its comment).
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
