package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"ccam/internal/switching"
)

// seedRunEnv points HOME at a temp dir with a two-account ~/.ccam and a shared
// ~/.claude, so cmdRun's real config/store/hook-install paths all operate on
// throwaway files.
func seedRunEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	mustMkdir(t, filepath.Join(home, ".ccam", "accounts"))
	mustMkdir(t, filepath.Join(home, ".claude"))
	ehtiDir := filepath.Join(home, ".ccam", "accounts", "ehti")
	workDir := filepath.Join(home, ".ccam", "accounts", "work")
	mustMkdir(t, ehtiDir)
	mustMkdir(t, workDir)

	// Marshal rather than hand-build the JSON: on Windows the configDir paths
	// contain backslashes, which are invalid unescaped in a JSON string.
	accountsData, err := json.Marshal(map[string]any{"accounts": []map[string]any{
		{"id": "ehti", "slug": "ehti", "kind": "managed", "configDir": ehtiDir, "alias": "claude-ehti", "isolation": "credentials-only"},
		{"id": "work", "slug": "work", "kind": "managed", "configDir": workDir, "alias": "claude-work", "isolation": "credentials-only"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(home, ".ccam", "accounts.json"), string(accountsData))
	mustWrite(t, filepath.Join(home, ".claude.json"), `{"oauthAccount":{"accountUuid":"orig"}}`)
	// Give "work" an identity stub so the switch also exercises applyIdentity.
	mustWrite(t, filepath.Join(workDir, ".claude.json"), `{"oauthAccount":{"accountUuid":"work-uuid"}}`)
	return home
}

func mustMkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
}
func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRunSupervisorRelaunchesOnSwitch drives the whole supervisor loop with a
// stubbed launcher: first "launch" stages a switch to work, second exits. The
// loop must relaunch as work with the forked session resumed, and set work's
// identity active.
func TestRunSupervisorRelaunchesOnSwitch(t *testing.T) {
	home := seedRunEnv(t)

	var calls [][]string
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		captured := append([]string{}, args...)
		calls = append(calls, captured)
		if len(calls) == 1 {
			// Simulate the in-session hook staging a switch to "work".
			if err := switching.WriteHandoff(handoff, switching.Handoff{Account: "work", SessionID: "sess-123"}); err != nil {
				t.Fatal(err)
			}
			return 0, true
		}
		return 0, false
	}

	if code := cmdRun([]string{"ehti"}); code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	if len(calls) != 2 {
		t.Fatalf("want 2 launches (ehti, then work), got %d: %v", len(calls), calls)
	}
	if len(calls[0]) != 0 {
		t.Errorf("first launch should carry no resume args, got %v", calls[0])
	}
	want := []string{"--resume", "sess-123", "--fork-session"}
	if !reflect.DeepEqual(calls[1], want) {
		t.Errorf("second launch args = %v, want %v", calls[1], want)
	}
	// The switch made work the active identity in the shared config.
	got := readJSONFile(t, filepath.Join(home, ".claude.json"))
	if oa, _ := got["oauthAccount"].(map[string]any); oa["accountUuid"] != "work-uuid" {
		t.Errorf("active identity = %v, want work-uuid", got["oauthAccount"])
	}
}

func TestRunUnknownAccount(t *testing.T) {
	seedRunEnv(t)
	if code := cmdRun([]string{"nope"}); code == 0 {
		t.Error("running an unknown account should fail")
	}
}

func TestRunClaudeOnceTerminatesOnHandoff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake claude is POSIX")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	mustWrite(t, fake, "#!/bin/sh\nsleep 30\n")
	os.Chmod(fake, 0o755)
	handoff := filepath.Join(dir, "handoff.json")

	// Stage a switch shortly after launch; the supervisor should notice and
	// terminate the sleeping child.
	go func() {
		time.Sleep(300 * time.Millisecond)
		switching.WriteHandoff(handoff, switching.Handoff{Account: "work", SessionID: "s"})
	}()

	done := make(chan bool, 1)
	go func() {
		_, switched := runClaudeOnce(fake, nil, os.Environ(), handoff)
		done <- switched
	}()
	select {
	case switched := <-done:
		if !switched {
			t.Error("runClaudeOnce should report switched=true when a handoff appears")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runClaudeOnce did not terminate the child on a handoff")
	}
}

func TestRunClaudeOnceReturnsChildExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake claude is POSIX")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	mustWrite(t, fake, "#!/bin/sh\nexit 7\n")
	os.Chmod(fake, 0o755)

	code, switched := runClaudeOnce(fake, nil, os.Environ(), filepath.Join(dir, "no-handoff.json"))
	if switched {
		t.Error("a clean exit is not a switch")
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7 (the child's)", code)
	}
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
