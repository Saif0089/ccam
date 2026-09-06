//go:build !windows

package cli

import "os"

// deleteSelfBinary removes path, ccam's own currently-running
// executable. Unix allows unlinking a running binary — the process
// keeps running from its already-open inode until it exits — so this
// is just a plain remove.
func deleteSelfBinary(path string) error {
	return os.Remove(path)
}
