// Package editors points VS Code and its relatives at a ccam account.
//
// The Claude Code extension does not go through the user's shell — it spawns
// Claude itself, taking the environment from the extension host and then
// applying its own `claudeCode.environmentVariables` setting LAST, so that
// setting is the one thing that reliably decides which account an extension
// conversation runs as. ccam writes a single entry there, pointing at a
// credential store of its own; switching afterwards is a write to that store,
// which a running conversation picks up exactly as a terminal session does.
//
// The setting is machine-scoped, so it lives in the editor's user settings.json
// — a file the user owns, with their comments and formatting in it. Nothing
// here reformats that file: the value is edited in place, and everything around
// it is left byte for byte as it was.
package editors

import (
	"os"
	"path/filepath"
	"runtime"
)

// EnvSetting is the setting the Claude Code extension applies over the
// environment of every Claude process it starts.
const EnvSetting = "claudeCode.environmentVariables"

// Editor is one installed VS Code-family editor.
type Editor struct {
	Name     string // what to call it when talking to the user
	Settings string // its user settings.json
}

// products maps an editor's display name to the directory it keeps user data
// in. They are all VS Code forks and all use the same layout.
var products = []struct{ name, dir string }{
	{"VS Code", "Code"},
	{"VS Code Insiders", "Code - Insiders"},
	{"Cursor", "Cursor"},
	{"VSCodium", "VSCodium"},
	{"Windsurf", "Windsurf"},
}

// Installed lists the editors that actually exist on this machine, judged by
// whether their user-data directory is there. An editor that has never been
// run has nothing for ccam to configure and is skipped.
func Installed(home string) []Editor {
	root := userDataRoot(home)
	if root == "" {
		return nil
	}
	var found []Editor
	for _, p := range products {
		dir := filepath.Join(root, p.dir, "User")
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		found = append(found, Editor{Name: p.name, Settings: filepath.Join(dir, "settings.json")})
	}
	return found
}

// userDataRoot is where this platform's VS Code family keeps per-user data.
func userDataRoot(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return appData
		}
		return filepath.Join(home, "AppData", "Roaming")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return xdg
		}
		return filepath.Join(home, ".config")
	}
}
