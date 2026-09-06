package accounts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Prober checks whether an account's config directory holds a successful
// login. It's OS-agnostic on purpose: on Linux/Windows Claude Code always
// writes a credentials file under CLAUDE_CONFIG_DIR, but on macOS it may
// instead (or also) rely on the Keychain, in which case no file appears —
// so the fallback is a short, non-interactive `claude` invocation rather
// than any macOS-specific Keychain code.
type Prober struct {
	// ClaudeBinary is the executable to invoke for the fallback probe,
	// normally "claude" resolved via PATH. Overridable in tests.
	ClaudeBinary string
	// Timeout bounds the fallback probe so a hung/prompting process
	// can't block the caller forever.
	Timeout time.Duration
}

// NewProber returns a Prober using the "claude" binary on PATH with a
// sane default timeout.
func NewProber() *Prober {
	return &Prober{ClaudeBinary: "claude", Timeout: 10 * time.Second}
}

// IsLinked reports whether configDir already holds valid credentials.
func (p *Prober) IsLinked(ctx context.Context, configDir string) bool {
	if hasCredentialsFile(configDir) {
		return true
	}
	return p.headlessProbeSucceeds(ctx, configDir)
}

func hasCredentialsFile(configDir string) bool {
	info, err := os.Stat(filepath.Join(configDir, ".credentials.json"))
	return err == nil && info.Size() > 0
}

// headlessProbeSucceeds runs a minimal non-interactive claude invocation
// scoped to configDir. A login-less/expired session errors or prompts
// (which, run without a TTY, exits non-zero) rather than answering, so a
// clean exit 0 is a reliable "already logged in" signal.
func (p *Prober) headlessProbeSucceeds(ctx context.Context, configDir string) bool {
	binary := p.ClaudeBinary
	if binary == "" {
		binary = "claude"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "-p", "ping", "--max-turns", "1")
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir)
	return cmd.Run() == nil
}
