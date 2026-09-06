package service

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"

	"ccam/internal/claudebin"
)

// servicePATH is the PATH baked into the autostart entry at install
// time. Login-started services get a near-empty PATH (launchd hands out
// roughly /usr/bin:/bin:/usr/sbin:/sbin), but ccam has to run `claude`,
// which normally lives under the user's home and is itself a Node
// program that needs the rest of the user's PATH. The shell running
// `ccam install` is by definition an environment where the user's
// `claude` works, so its PATH is the right thing to persist — plus the
// directory of whatever claude we actually resolved, in case the user
// installed ccam from a shell that couldn't see it either.
func servicePATH() string {
	path := os.Getenv("PATH")
	claude := claudebin.Resolve()
	if !filepath.IsAbs(claude) {
		return path
	}

	dir := filepath.Dir(claude)
	for _, element := range filepath.SplitList(path) {
		if element == dir {
			return path
		}
	}
	if path == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + path
}

// serviceWorkingDir is the directory the service (and therefore every
// `claude` it spawns) runs in. Without this, launchd would start it in
// "/" and `claude` would treat the filesystem root as its project
// directory — prompting for trust and scanning far too much.
func serviceWorkingDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return string(filepath.Separator)
	}
	return home
}

// xmlEscape makes a string safe to interpolate into the LaunchAgent
// plist. A home directory containing "&" would otherwise produce a
// plist launchd refuses to parse — silently, at the next login.
func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}
