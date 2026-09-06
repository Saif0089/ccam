// Package termlauncher opens a new, visible terminal window already
// scoped (via CLAUDE_CONFIG_DIR) to one account, for people who'd rather
// click a button than remember/copy a shell alias.
package termlauncher

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Launch opens a new terminal window running `claude` with
// CLAUDE_CONFIG_DIR set to configDir. label is shown in the window title
// where the OS/terminal supports it.
func Launch(configDir, label string) error {
	switch runtime.GOOS {
	case "darwin":
		return launchDarwin(configDir, label)
	case "windows":
		return launchWindows(configDir, label)
	default:
		return launchLinux(configDir, label)
	}
}

func launchDarwin(configDir, label string) error {
	script := fmt.Sprintf(
		`tell application "Terminal" to do script "env CLAUDE_CONFIG_DIR=%s claude; exec $SHELL" activate`,
		appleScriptQuote(configDir),
	)
	_ = label // Terminal.app tab titles aren't reliably settable from here.
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Start()
}

func appleScriptQuote(s string) string {
	out := ""
	for _, r := range s {
		switch r {
		case '\\', '"':
			out += `\` + string(r)
		default:
			out += string(r)
		}
	}
	return out
}

// linuxTerminals is tried in order; the first one found on PATH is used.
// Each entry's args open a new window running shellCmd.
var linuxTerminals = []struct {
	bin  string
	args func(shellCmd string) []string
}{
	{"x-terminal-emulator", func(c string) []string { return []string{"-e", "sh", "-c", c} }},
	{"gnome-terminal", func(c string) []string { return []string{"--", "sh", "-c", c} }},
	{"konsole", func(c string) []string { return []string{"-e", "sh", "-c", c} }},
	{"xfce4-terminal", func(c string) []string { return []string{"-e", c} }},
	{"xterm", func(c string) []string { return []string{"-e", c} }},
}

func launchLinux(configDir, label string) error {
	_ = label
	shellCmd := fmt.Sprintf("env CLAUDE_CONFIG_DIR=%s claude; exec $SHELL", shellQuote(configDir))
	var lastErr error
	for _, t := range linuxTerminals {
		if _, err := exec.LookPath(t.bin); err != nil {
			continue
		}
		cmd := exec.Command(t.bin, t.args(shellCmd)...)
		if err := cmd.Start(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("no working terminal emulator found: %w", lastErr)
	}
	return fmt.Errorf("no terminal emulator found on PATH (tried x-terminal-emulator, gnome-terminal, konsole, xfce4-terminal, xterm)")
}

func shellQuote(s string) string {
	return "'" + replaceAll(s, "'", `'\''`) + "'"
}

func replaceAll(s, old, new string) string {
	out := ""
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			out += new
			i += len(old)
		} else {
			out += string(s[i])
			i++
		}
	}
	return out
}

func launchWindows(configDir, label string) error {
	_ = label
	psCmd := fmt.Sprintf(`$env:CLAUDE_CONFIG_DIR='%s'; claude`, psQuote(configDir))

	if wt, err := exec.LookPath("wt.exe"); err == nil {
		cmd := exec.Command(wt, "powershell", "-NoExit", "-Command", psCmd)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}

	cmd := exec.Command("powershell", "-NoExit", "-Command", psCmd)
	return cmd.Start()
}

func psQuote(s string) string {
	return replaceAll(s, "'", "''")
}
