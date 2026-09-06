package claudebin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withEnv sets env vars for the duration of a test.
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	restore := map[string]string{}
	for k, v := range kv {
		restore[k] = os.Getenv(k)
		os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k, v := range restore {
			os.Setenv(k, v)
		}
	})
}

func writeStubClaude(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	return path
}

// TestResolveFindsClaudeWithoutItBeingOnPATH is the regression guard for
// the bug that broke logins after every reboot: started by launchd (or
// systemd, or a Windows Startup entry) at login, ccam gets a bare PATH
// that does not include ~/.local/bin, where claude actually lives, and
// a plain exec.LookPath("claude") fails.
func TestResolveFindsClaudeWithoutItBeingOnPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the unix ~/.local/bin layout")
	}
	home := t.TempDir()
	want := writeStubClaude(t, filepath.Join(home, ".local", "bin"))

	withEnv(t, map[string]string{
		"HOME":      home,
		"PATH":      "/usr/bin:/bin:/usr/sbin:/sbin", // what launchd hands out
		EnvOverride: "",
	})

	if got := Resolve(); got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolvePrefersExplicitOverride(t *testing.T) {
	home := t.TempDir()
	writeStubClaude(t, filepath.Join(home, ".local", "bin"))
	override := writeStubClaude(t, filepath.Join(home, "custom"))

	withEnv(t, map[string]string{
		"HOME":      home,
		EnvOverride: override,
	})

	if got := Resolve(); got != override {
		t.Errorf("Resolve() = %q, want the override %q", got, override)
	}
}

func TestResolvePrefersPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the unix ~/.local/bin layout")
	}
	home := t.TempDir()
	writeStubClaude(t, filepath.Join(home, ".local", "bin"))
	onPath := writeStubClaude(t, filepath.Join(home, "bin"))

	withEnv(t, map[string]string{
		"HOME":      home,
		"PATH":      filepath.Dir(onPath),
		EnvOverride: "",
	})

	if got := Resolve(); got != onPath {
		t.Errorf("Resolve() = %q, want the one on PATH %q", got, onPath)
	}
}

// TestResolveFallsBackToBareName keeps the failure mode useful: with no
// claude anywhere, callers should still get "claude" so their own error
// says "executable file not found" rather than something cryptic.
func TestResolveFallsBackToBareName(t *testing.T) {
	home := t.TempDir()
	withEnv(t, map[string]string{
		"HOME":      home,
		"PATH":      filepath.Join(home, "empty"),
		EnvOverride: "",
	})

	if got := Resolve(); got != "claude" {
		t.Errorf("Resolve() = %q, want %q", got, "claude")
	}
}
