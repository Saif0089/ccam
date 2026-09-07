package shellrc

import (
	"os"
	"path/filepath"
	"runtime"
)

// RcPaths returns every rc file ccam keeps in sync, given the user's
// home directory. Writing to a shell's rc file even when that shell
// isn't installed is harmless (it's just an unused file); the goal is
// that whichever shell the user opens next already has every account's
// alias available.
func RcPaths(homeDir string) map[Shell]string {
	paths := map[Shell]string{}
	switch runtime.GOOS {
	case "windows":
		// PowerShell (7+) profile; Windows PowerShell 5.1 uses a
		// different default path some setups still rely on, so keep
		// both in sync.
		docs := filepath.Join(homeDir, "Documents")
		paths[PowerShell] = filepath.Join(docs, "PowerShell", "Microsoft.PowerShell_profile.ps1")
	default:
		paths[Bash] = filepath.Join(homeDir, ".bashrc")
		paths[Zsh] = filepath.Join(homeDir, ".zshrc")
		paths[Fish] = filepath.Join(homeDir, ".config", "fish", "config.fish")
		// pwsh (PowerShell Core) on macOS/Linux, for users who have it.
		paths[PowerShell] = filepath.Join(homeDir, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	}
	return paths
}

// OptionalRcPaths returns login-shell startup files that are managed
// only when they already exist.
//
// A bash *login* shell — what macOS Terminal.app starts, and what an
// ssh session gets — reads ~/.bash_profile or ~/.profile and never
// touches ~/.bashrc, so an alias written only to .bashrc silently isn't
// there for those users. These are never created, though: creating a
// ~/.bash_profile where none existed would stop bash reading ~/.profile
// at all, which is a much worse thing to do to someone's shell than
// missing an alias.
func OptionalRcPaths(homeDir string) map[Shell]string {
	if runtime.GOOS == "windows" {
		return map[Shell]string{}
	}
	return map[Shell]string{
		Bash:      filepath.Join(homeDir, ".bash_profile"),
		BashLogin: filepath.Join(homeDir, ".profile"),
	}
}

// Syncer keeps the managed alias block in every rc file under HomeDir
// consistent with the current account list.
type Syncer struct {
	HomeDir string
}

// NewSyncer builds a Syncer rooted at homeDir (normally the user's real
// home directory, os.UserHomeDir()).
func NewSyncer(homeDir string) *Syncer {
	return &Syncer{HomeDir: homeDir}
}

// Sync rewrites the managed block in every rc file to match entries
// exactly. Call it after any account add/rename/remove. If entries is
// empty, the managed block (if any) is removed rather than left empty.
func (s *Syncer) Sync(entries []AliasEntry) error {
	for shell, path := range RcPaths(s.HomeDir) {
		if err := s.sync(shell, path, entries); err != nil {
			return err
		}
	}
	// Login-shell files are only updated when the user already has them.
	for shell, path := range OptionalRcPaths(s.HomeDir) {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := s.sync(shell, path, entries); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) sync(shell Shell, path string, entries []AliasEntry) error {
	if len(entries) == 0 {
		return RemoveBlock(path)
	}
	return UpsertBlock(path, RenderBody(shell, entries))
}

// RemoveAll strips the managed block from every rc file, used by
// `ccam uninstall` so no generated content is left behind.
func (s *Syncer) RemoveAll() error {
	for _, paths := range []map[Shell]string{RcPaths(s.HomeDir), OptionalRcPaths(s.HomeDir)} {
		for _, path := range paths {
			if err := RemoveBlock(path); err != nil {
				return err
			}
		}
	}
	return nil
}
