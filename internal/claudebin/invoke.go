package claudebin

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Invocation turns a resolved claude path plus arguments into the
// command that actually runs it.
//
// On Windows this matters: `npm i -g @anthropic-ai/claude-code` installs
// a `claude.cmd` shim, and CreateProcess — which is what both os/exec
// and ConPTY end up calling — cannot execute a .cmd or .bat directly
// ("%1 is not a valid Win32 application"). Batch files have to be run
// through cmd.exe. Everywhere else, and for a real .exe, this is a
// no-op.
func Invocation(binary string, args []string) (string, []string) {
	if runtime.GOOS != "windows" {
		return binary, args
	}

	switch strings.ToLower(filepath.Ext(binary)) {
	case ".cmd", ".bat":
		return "cmd.exe", append([]string{"/c", binary}, args...)
	default:
		return binary, args
	}
}
