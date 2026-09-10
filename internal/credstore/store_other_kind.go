//go:build !darwin

package credstore

// KeychainBacked is always false where the store is a file: Claude Code re-reads
// it on every request, so a switch is visible immediately.
func KeychainBacked(string) bool { return false }
