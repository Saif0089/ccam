// Package ptyio starts a child process attached to a pseudo-terminal.
// It exists because `claude`'s interactive login flow needs a real TTY
// to render (it hangs on a plain pipe) — see docs/ARCHITECTURE.md. Unix
// and Windows need genuinely different OS APIs (a real pty vs the
// ConPTY API), so this package is the one place that split lives; every
// caller only sees the Session interface below.
package ptyio

import "context"

// The pseudo-terminal is deliberately far wider than a real one. The
// only thing ccam reads off it is `claude`'s OAuth URL, which is ~600
// characters — at a normal 80/120-column width the CLI hard-wraps it
// across several screen lines, and anything scraping the rendered
// screen then gets a truncated, broken URL. Sizing the terminal past
// the longest line we ever expect keeps the URL on one line.
const (
	DefaultCols = 1000
	DefaultRows = 50
)

// Session is a running child process attached to a pseudo-terminal.
type Session interface {
	// Read returns bytes the child process wrote to the terminal,
	// including its own echo of anything written to it.
	Read(p []byte) (int, error)
	// Write sends bytes to the child process as if typed at the terminal.
	Write(p []byte) (int, error)
	// Close releases the terminal and, on most platforms, ends the
	// child process along with it.
	Close() error
	// Wait blocks until the child process exits or ctx is done,
	// returning its exit code.
	Wait(ctx context.Context) (int, error)
	// Resize updates the terminal's reported size.
	Resize(cols, rows int) error
}

// Start launches name with args and env (in "KEY=VALUE" form, replacing
// the child's entire environment — pass os.Environ() plus overrides to
// extend rather than replace it) attached to a new pseudo-terminal, in
// the working directory dir.
//
// dir matters more than it looks: started by launchd at login, ccam's
// own working directory is "/", and `claude` inherits it and treats the
// filesystem root as the project directory — prompting for trust and
// scanning far too much. Pass the user's home directory.
func Start(name string, args []string, env []string, dir string) (Session, error) {
	return start(name, args, env, dir)
}
