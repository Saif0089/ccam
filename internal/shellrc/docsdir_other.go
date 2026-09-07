//go:build !windows

package shellrc

import "path/filepath"

// documentsDir is only meaningful on Windows; the non-Windows build
// keeps the signature so callers don't need build tags.
func documentsDir(homeDir string) string {
	return filepath.Join(homeDir, "Documents")
}
