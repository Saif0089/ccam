//go:build windows

package shellrc

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// documentsDir returns the Documents folder for homeDir, where
// PowerShell keeps its profiles.
//
// Not simply homeDir\Documents: with OneDrive's Known Folder Move —
// common, and on by default for many Microsoft accounts — the real
// folder is somewhere like %USERPROFILE%\OneDrive\Documents, and a
// profile written to the un-redirected path is never read.
//
// The registry value is authoritative for the *current* user, so it is
// only used when it actually describes homeDir: either because it
// resolves to somewhere inside it (the normal case, OneDrive included),
// or because homeDir is the current user's profile, in which case even
// an off-profile redirect (a network share) is right. Anything else
// means we were asked about a different home than the one the registry
// describes — a test with a fake HOME — and the answer has to stay
// inside the home we were given.
func documentsDir(homeDir string) string {
	fallback := filepath.Join(homeDir, "Documents")

	personal := shellFolderPersonal(homeDir)
	if personal == "" {
		return fallback
	}
	if underDir(personal, homeDir) || sameDir(homeDir, os.Getenv("USERPROFILE")) {
		return personal
	}
	return fallback
}

// shellFolderPersonal reads the user's Documents path from the registry,
// expanding %USERPROFILE% against homeDir so a fake home is honoured.
func shellFolderPersonal(homeDir string) string {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	// User Shell Folders holds the unexpanded form (REG_EXPAND_SZ),
	// which is what makes the %USERPROFILE% substitution below possible.
	personal, _, err := key.GetStringValue("Personal")
	if err != nil || personal == "" {
		return ""
	}

	personal = strings.ReplaceAll(personal, "%USERPROFILE%", homeDir)
	return os.ExpandEnv(personal)
}

func underDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
