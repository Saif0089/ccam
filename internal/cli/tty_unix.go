//go:build !windows

package cli

import "golang.org/x/sys/unix"

// isatty reports whether fd is an interactive terminal. TIOCGWINSZ is the one
// terminal ioctl spelled the same way on every unix, and it fails with ENOTTY
// on anything that is not a tty — a pipe, a regular file, or /dev/null (which
// a plain os.Stat mode check would wrongly accept, since it is a chardev too).
func isatty(fd uintptr) bool {
	_, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	return err == nil
}
