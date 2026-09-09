package accounts

import (
	"strings"
	"testing"
)

// valueOf returns the value of name in env, and whether it was present at
// all. Presence and value are separate answers on purpose: Claude Code
// branches on whether CLAUDE_SECURESTORAGE_CONFIG_DIR is *set*, not on
// what it holds, so "absent" and "present but empty" are different
// identities and a test that conflates them proves nothing.
func valueOf(env []string, name string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		key, rest, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, name) {
			// Keep scanning: a later duplicate is what os/exec would
			// resolve to, so the last one is the one that matters.
			value, found = rest, true
		}
	}
	return value, found
}

func TestEnvForConfigDirSetsBothVariablesToTheSamePath(t *testing.T) {
	dir := "/home/me/.ccam/accounts/work"
	env := EnvForConfigDir(dir)

	for _, name := range []string{ConfigDirEnvVar, SecureStorageEnvVar} {
		got, ok := valueOf(env, name)
		if !ok {
			t.Fatalf("%s is absent; login and probe must pin the config directory explicitly", name)
		}
		if got != dir {
			t.Errorf("%s = %q, want %q", name, got, dir)
		}
	}
}

// TestEnvForSharedConfigLeavesTheConfigDirUnset is the whole point of the
// swap: the credential store moves, the config directory does not, so the
// session keeps the user's sessions, MCP servers, skills and hooks.
func TestEnvForSharedConfigLeavesTheConfigDirUnset(t *testing.T) {
	dir := "/home/me/.ccam/accounts/work"
	env := EnvForSharedConfig(dir)

	got, ok := valueOf(env, SecureStorageEnvVar)
	if !ok {
		t.Fatalf("%s is absent; the session would run as the default login", SecureStorageEnvVar)
	}
	if got != dir {
		t.Errorf("%s = %q, want %q", SecureStorageEnvVar, got, dir)
	}
	if value, ok := valueOf(env, ConfigDirEnvVar); ok {
		t.Errorf("%s is set to %q; setting it isolates the config directory, which is exactly "+
			"what sharing ~/.claude is meant to stop", ConfigDirEnvVar, value)
	}
}

// TestEnvNeverEmitsAnEmptyValue guards the footgun that makes this whole
// change dangerous to get wrong. Claude Code resolves
// CLAUDE_SECURESTORAGE_CONFIG_DIR="" to the bare "Claude Code-credentials"
// item — the user's real default login — and the next token refresh would
// rotate that login's single-use refresh token out from under them. An
// empty configDir means the default account, which is reached by removing
// both names, never by setting either to "".
func TestEnvNeverEmitsAnEmptyValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
	}{
		{"EnvForConfigDir", EnvForConfigDir("")},
		{"EnvForSharedConfig", EnvForSharedConfig("")},
	} {
		for _, name := range []string{ConfigDirEnvVar, SecureStorageEnvVar} {
			if value, ok := valueOf(tc.env, name); ok {
				t.Errorf("%s(\"\") set %s=%q; the default account is reached by removing the "+
					"variable, and an empty value resolves to the user's real login", tc.name, name, value)
			}
		}
	}
}

// TestEnvStripsInheritedValues covers the ConPTY hazard documented on
// EnvForConfigDir: Windows resolves a duplicate name to the *first*
// match, so shadowing an inherited value is not enough — it has to be
// removed. Both names are stripped by both constructors, including the
// one each function does not go on to set.
func TestEnvStripsInheritedValues(t *testing.T) {
	t.Setenv(ConfigDirEnvVar, "/inherited/config")
	t.Setenv(SecureStorageEnvVar, "/inherited/credentials")

	dir := "/home/me/.ccam/accounts/work"
	for _, tc := range []struct {
		name string
		env  []string
		want map[string]string // name -> expected value; absent means must not be set
	}{
		{"EnvForConfigDir", EnvForConfigDir(dir), map[string]string{
			ConfigDirEnvVar: dir, SecureStorageEnvVar: dir,
		}},
		{"EnvForSharedConfig", EnvForSharedConfig(dir), map[string]string{
			SecureStorageEnvVar: dir,
		}},
		{"EnvForConfigDir default", EnvForConfigDir(""), nil},
		{"EnvForSharedConfig default", EnvForSharedConfig(""), nil},
	} {
		for _, name := range []string{ConfigDirEnvVar, SecureStorageEnvVar} {
			got, ok := valueOf(tc.env, name)
			want, wanted := tc.want[name]
			switch {
			case wanted && !ok:
				t.Errorf("%s: %s absent, want %q", tc.name, name, want)
			case wanted && got != want:
				t.Errorf("%s: %s = %q, want %q — an inherited value survived", tc.name, name, got, want)
			case !wanted && ok:
				t.Errorf("%s: %s = %q, want absent — the inherited value was not stripped",
					tc.name, name, got)
			}
			// Count occurrences too: leaving the inherited entry in
			// place and appending a second one reads correctly under
			// os/exec and wrongly under ConPTY.
			if n := countEntries(tc.env, name); n > 1 {
				t.Errorf("%s: %s appears %d times; Windows ConPTY resolves to the first match",
					tc.name, name, n)
			}
		}
	}
}

func countEntries(env []string, name string) int {
	n := 0
	for _, entry := range env {
		if key, _, ok := strings.Cut(entry, "="); ok && strings.EqualFold(key, name) {
			n++
		}
	}
	return n
}
