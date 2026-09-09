package cli

import "golang.org/x/sys/windows"

// isatty reports whether fd is an interactive console. GetConsoleMode only
// succeeds on a real console handle, so a pipe or NUL fails it.
func isatty(fd uintptr) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
}
