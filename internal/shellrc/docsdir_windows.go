//go:build windows

package shellrc

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// documentsDir returns the user's Documents folder, where PowerShell
// keeps its profiles.
//
// Not homeDir\Documents: with OneDrive's Known Folder Move enabled —
// which is common, and on by default for many Microsoft accounts — the
// real folder is under %USERPROFILE%\OneDrive\Documents, and a profile
// written to the un-redirected path is simply never read.
func documentsDir(homeDir string) string {
	fallback := filepath.Join(homeDir, "Documents")

	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders`, registry.QUERY_VALUE)
	if err != nil {
		return fallback
	}
	defer key.Close()

	personal, _, err := key.GetStringValue("Personal")
	if err != nil || personal == "" {
		return fallback
	}
	return os.ExpandEnv(strings.ReplaceAll(personal, "%USERPROFILE%", homeDir))
}
