package shellrc

import (
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
		if len(entries) == 0 {
			if err := RemoveBlock(path); err != nil {
				return err
			}
			continue
		}
		if err := UpsertBlock(path, RenderBody(shell, entries)); err != nil {
			return err
		}
	}
	return nil
}

// RemoveAll strips the managed block from every rc file, used by
// `ccam uninstall` so no generated content is left behind.
func (s *Syncer) RemoveAll() error {
	for _, path := range RcPaths(s.HomeDir) {
		if err := RemoveBlock(path); err != nil {
			return err
		}
	}
	return nil
}
