//go:build !windows

package updater

// CleanupOldBinary exists only for Windows, where a running executable
// has to be renamed aside rather than overwritten. Everywhere else the
// rename replaces the file outright and there is nothing left behind.
func CleanupOldBinary(string) {}
