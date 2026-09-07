package accounts

import (
	"os"
	"strings"
)

// ConfigDirEnvVar is the variable Claude Code reads to decide which
// config directory — and therefore which account — to use.
const ConfigDirEnvVar = "CLAUDE_CONFIG_DIR"

// EnvForConfigDir builds the environment for running `claude` against
// one account.
//
// Two things it must get right, both learned the hard way:
//
// An empty configDir means the *default* account, and that is reached
// by removing the variable, not by setting it to the default path. On
// macOS the credentials are keyed by config dir, so
// CLAUDE_CONFIG_DIR=~/.claude reports logged out while plain `claude`
// is logged in — they are different identities as far as the CLI is
// concerned.
//
// And any inherited value is stripped rather than shadowed. os/exec
// resolves duplicate keys to the last one, but the ConPTY path on
// Windows builds its environment block verbatim and Windows resolves
// to the *first* match — so a user who already has CLAUDE_CONFIG_DIR
// set globally (exactly the manual workflow ccam replaces) would have
// every login silently run against their own global config.
func EnvForConfigDir(configDir string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, ConfigDirEnvVar+"=") {
			continue
		}
		out = append(out, entry)
	}
	if configDir != "" {
		out = append(out, ConfigDirEnvVar+"="+configDir)
	}
	return out
}
