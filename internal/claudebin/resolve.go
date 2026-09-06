// Package claudebin locates the `claude` executable.
//
// This exists because of where ccam runs from: at login the background
// service is started by launchd / systemd --user / a Startup entry, none
// of which give it the user's shell PATH. launchd in particular hands a
// process roughly `/usr/bin:/bin:/usr/sbin:/sbin`, while `claude` is
// typically installed under the user's home (~/.local/bin). A plain
// exec.LookPath("claude") therefore works when ccam is started from a
// terminal and fails after every reboot — so we also look in the places
// Claude Code actually installs itself.
package claudebin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// EnvOverride lets a user point ccam at a specific claude executable.
const EnvOverride = "CCAM_CLAUDE_BIN"

// Resolve returns the path to the claude executable. It prefers an
// explicit override, then PATH, then the known install locations. If
// nothing is found it returns "claude" so callers still produce a
// sensible "not found" error when they try to run it.
func Resolve() string {
	if override := os.Getenv(EnvOverride); override != "" {
		return override
	}
	if path, err := exec.LookPath("claude"); err == nil {
		return path
	}
	for _, candidate := range candidates() {
		if isExecutableFile(candidate) {
			return candidate
		}
	}
	return "claude"
}

// candidates lists the usual install locations, most likely first.
func candidates() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	var paths []string
	add := func(parts ...string) {
		if parts[0] == "" {
			return
		}
		paths = append(paths, filepath.Join(parts...))
	}

	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		localAppData := os.Getenv("LOCALAPPDATA")
		add(localAppData, "Programs", "claude", "claude.exe")
		add(appData, "npm", "claude.cmd")
		add(appData, "npm", "claude.exe")
		add(home, ".local", "bin", "claude.exe")
		add(home, ".bun", "bin", "claude.exe")
		return paths
	}

	// Claude Code's own installer, npm/bun global installs, Homebrew.
	add(home, ".local", "bin", "claude")
	add(home, ".claude", "local", "claude")
	add(home, ".bun", "bin", "claude")
	add(home, ".npm-global", "bin", "claude")
	add(home, "node_modules", ".bin", "claude")
	paths = append(paths,
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
		"/usr/bin/claude",
	)
	return paths
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true // no executable bit to check
	}
	return info.Mode().Perm()&0o111 != 0
}
