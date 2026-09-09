package accounts

import (
	"os"
	"strings"
)

// ConfigDirEnvVar is the variable Claude Code reads to decide which
// config directory to use: sessions, MCP servers, skills, plugins,
// hooks, agents and the projects/ transcripts all live under it.
const ConfigDirEnvVar = "CLAUDE_CONFIG_DIR"

// SecureStorageEnvVar is the variable Claude Code reads to decide where
// the *credentials* live, independently of the config directory. It is
// what lets ccam swap identity without swapping everything else.
//
// Claude Code derives both the macOS Keychain item name and the
// file-based store's path from it:
//
//	function jy(){let n=process.env.CLAUDE_SECURESTORAGE_CONFIG_DIR;
//	  if(n!==void 0)return(n||l(o(),".claude")).normalize("NFC");
//	  return be()}
//
// with the Keychain service name being
// "Claude Code-credentials-<first 8 hex of sha256(that path)>". The
// hash input is the same path string ccam already exports as
// CLAUDE_CONFIG_DIR, so switching an account from one variable to the
// other keeps its existing credentials: no re-login, on any OS.
const SecureStorageEnvVar = "CLAUDE_SECURESTORAGE_CONFIG_DIR"

// EnvForConfigDir builds the environment for running `claude` against
// one account with that account's directory as the *config* directory —
// the isolation ccam has always used. Both variables are set, to the
// same path.
//
// Two things it must get right, both learned the hard way:
//
// An empty configDir means the *default* account, and that is reached
// by removing the variables, not by setting them to the default path.
// Credentials are keyed by the directory, so CLAUDE_CONFIG_DIR=~/.claude
// reports logged out while plain `claude` is logged in — they are
// different identities as far as the CLI is concerned. Worse for the
// new variable: the CLI branches on its *presence*, not its value, so
// SecureStorageEnvVar="" resolves to the bare "Claude Code-credentials"
// item, which is the user's real default login. Never emit either name
// with an empty value.
//
// And any inherited value is stripped rather than shadowed. os/exec
// resolves duplicate keys to the last one, but the ConPTY path on
// Windows builds its environment block verbatim and Windows resolves
// to the *first* match — so a user who already has CLAUDE_CONFIG_DIR
// set globally (exactly the manual workflow ccam replaces) would have
// every login silently run against their own global config.
//
// Why both, rather than just CLAUDE_CONFIG_DIR as this used to do: the
// two variables select the same credential store by different routes,
// and the routes disagree on normalisation. The Keychain derivation
// NFC-normalises the securestorage path but hashes the config-dir path
// raw, so an account directory whose name is not already in NFC (any
// non-ASCII path — an accented or non-Latin username) would hash to one
// item when logging in and a different one when running a session.
// Setting both pins every process to the NFC branch.
func EnvForConfigDir(configDir string) []string {
	if configDir == "" {
		return envWithout(ConfigDirEnvVar, SecureStorageEnvVar)
	}
	return append(envWithout(ConfigDirEnvVar, SecureStorageEnvVar),
		ConfigDirEnvVar+"="+configDir,
		SecureStorageEnvVar+"="+configDir,
	)
}

// EnvForSharedConfig builds the environment for running `claude` against
// one account while leaving the config directory shared — the swap this
// exists for. Only the credential store moves: CLAUDE_CONFIG_DIR is left
// unset, so the session reads and writes the user's own ~/.claude, and
// keeps its sessions, MCP servers, skills, plugins, hooks, agents and
// CLAUDE.md. Only the login is this account's.
//
// The same two rules as EnvForConfigDir apply, and the empty-value one
// matters more here because it is the only variable being set: emitting
// SecureStorageEnvVar="" would point the session at the user's default
// login, and the next token refresh would rotate that login's refresh
// token under it. An empty configDir therefore means the default
// account and strips both names, exactly as above.
func EnvForSharedConfig(configDir string) []string {
	if configDir == "" {
		return envWithout(ConfigDirEnvVar, SecureStorageEnvVar)
	}
	return append(envWithout(ConfigDirEnvVar, SecureStorageEnvVar),
		SecureStorageEnvVar+"="+configDir,
	)
}

// envWithout returns the process environment with the named variables
// removed. Names are compared case-insensitively because Windows
// environment names are, and only the name is compared: a value that
// happens to contain "CLAUDE_CONFIG_DIR=" must not cause its variable
// to be dropped.
func envWithout(names ...string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(names))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if ok && matchesAny(name, names) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func matchesAny(name string, names []string) bool {
	for _, candidate := range names {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}
