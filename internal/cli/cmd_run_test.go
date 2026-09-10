package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
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

	// A ccam-supervised shell would export a handoff path; clear it so these
	// tests exercise the supervisor rather than the switch-staging path.
	t.Setenv(switching.HandoffEnvVar, "")

	// `go test` runs with stdin on a pipe. Claim the terminal so cmdRun's
	// interactive guard does not turn every supervisor test into a refusal.
	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	return home
}

// seedTranscript writes the file Claude Code would have written for a session,
// in the shared ~/.claude the accounts now pool into.
func seedTranscript(t *testing.T, home, sessionID string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "-some-project")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, sessionID+".jsonl"), "{}\n")
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
	// The session being switched has a conversation on disk, so the relaunch
	// forks it. (Without one there is nothing to resume — see the test below.)
	seedTranscript(t, home, "sess-123")

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

// The flags a session was started with have to survive the switch: dropping
// --dangerously-skip-permissions mid-conversation lands the user in a session
// that behaves differently from the one they were in.
func TestRunKeepsLaunchFlagsAcrossASwitch(t *testing.T) {
	home := seedRunEnv(t)
	seedTranscript(t, home, "sess-9")

	var calls [][]string
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		calls = append(calls, append([]string{}, args...))
		if len(calls) == 1 {
			if err := switching.WriteHandoff(handoff, switching.Handoff{Account: "work", SessionID: "sess-9"}); err != nil {
				t.Fatal(err)
			}
			return 0, true
		}
		return 0, false
	}

	if code := cmdRun([]string{"ehti", "--dangerously-skip-permissions"}); code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	want := []string{"--dangerously-skip-permissions", "--resume", "sess-9", "--fork-session"}
	if len(calls) != 2 || !reflect.DeepEqual(calls[1], want) {
		t.Errorf("relaunch args = %v, want %v", calls[len(calls)-1], want)
	}
}

// A session switched before it wrote anything has no conversation to carry.
// Resuming it anyway is what made Claude Code exit with "No conversation found
// with session ID" and drop the user back to the shell, so the relaunch must
// start clean instead.
func TestRunSwitchOfAnUnrecordedSessionStartsClean(t *testing.T) {
	seedRunEnv(t)

	var calls [][]string
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		calls = append(calls, append([]string{}, args...))
		if len(calls) == 1 {
			if err := switching.WriteHandoff(handoff, switching.Handoff{Account: "work", SessionID: "never-written"}); err != nil {
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
		t.Fatalf("want 2 launches, got %d: %v", len(calls), calls)
	}
	if len(calls[1]) != 0 {
		t.Errorf("relaunch args = %v, want none (nothing to resume)", calls[1])
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

// TestRunStagesSwitchInsideSupervisedSession is `!ccam work` typed in a session
// ccam is supervising: a shell command, so no tty and no hook payload, but the
// handoff path and session id are inherited from the session. It must stage the
// switch for the supervisor instead of starting a second session.
func TestRunStagesSwitchInsideSupervisedSession(t *testing.T) {
	home := seedRunEnv(t)
	handoff := filepath.Join(home, "handoff.json")
	t.Setenv(switching.HandoffEnvVar, handoff)
	t.Setenv(switching.SessionIDEnvVar, "sess-abc")
	// The supervisor has to still be running for a staged switch to mean
	// anything; this test process stands in for it.
	t.Setenv(switching.SupervisorEnvVar, strconv.Itoa(os.Getpid()))
	stdinIsTTY = func() bool { return false }

	launched := false
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		launched = true
		return 0, false
	}

	if code := cmdRun([]string{"work"}); code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	if launched {
		t.Error("staging a switch must not launch a nested Claude Code")
	}
	h, ok := switching.ReadHandoff(handoff)
	if !ok {
		t.Fatal("no handoff staged")
	}
	if h.Account != "work" || h.SessionID != "sess-abc" {
		t.Errorf("handoff = %+v, want account work / session sess-abc", h)
	}
}

// CCAM_HANDOFF is inherited by anything a session spawned, including processes
// that outlive it. Staging a handoff against a supervisor that has exited
// printed "Switching…" and did nothing at all.
func TestRunRefusesToStageForADeadSupervisor(t *testing.T) {
	home := seedRunEnv(t)
	handoff := filepath.Join(home, "handoff.json")
	t.Setenv(switching.HandoffEnvVar, handoff)
	t.Setenv(switching.SessionIDEnvVar, "sess-abc")
	t.Setenv(switching.SupervisorEnvVar, "999999") // no such process
	stdinIsTTY = func() bool { return false }

	if code := cmdRun([]string{"work"}); code == 0 {
		t.Error("staging against a dead supervisor should fail, got exit 0")
	}
	if _, ok := switching.ReadHandoff(handoff); ok {
		t.Error("nothing should have been staged")
	}
}

// `ccam ehti -p "..."` inside a supervised session is a deliberate one-shot on
// another account, not a request to switch the session and throw the arguments
// away.
func TestRunWithArgsInsideASessionDoesNotStageASwitch(t *testing.T) {
	home := seedRunEnv(t)
	handoff := filepath.Join(home, "handoff.json")
	t.Setenv(switching.HandoffEnvVar, handoff)
	t.Setenv(switching.SupervisorEnvVar, strconv.Itoa(os.Getpid()))
	stdinIsTTY = func() bool { return false }

	var got []string
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		got = append([]string{}, args...)
		return 0, false
	}

	if code := cmdRun([]string{"work", "-p", "hello"}); code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	if _, ok := switching.ReadHandoff(handoff); ok {
		t.Error("a command with arguments must not stage a switch")
	}
	if want := []string{"-p", "hello"}; !reflect.DeepEqual(got, want) {
		t.Errorf("launch args = %v, want %v", got, want)
	}
}

// TestRunRefusesWithoutATerminal covers `!ccam ehti` from inside a Claude Code
// session: no tty, no passthrough args, so ccam must explain itself rather than
// launch a Claude Code that dies on "Input must be provided...".
func TestRunRefusesWithoutATerminal(t *testing.T) {
	seedRunEnv(t)
	stdinIsTTY = func() bool { return false }

	launched := false
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		launched = true
		return 0, false
	}

	if code := cmdRun([]string{"ehti"}); code == 0 {
		t.Error("cmdRun without a terminal should fail, got exit 0")
	}
	if launched {
		t.Error("cmdRun should not launch Claude Code when there is no terminal")
	}
}

// A caller who passes Claude Code arguments is driving it deliberately
// (`ccam ehti -p "..."`), so the guard stays out of the way.
func TestRunWithArgsSkipsTheTerminalGuard(t *testing.T) {
	seedRunEnv(t)
	stdinIsTTY = func() bool { return false }

	var got []string
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(bin string, args, env []string, handoff string) (int, bool) {
		got = append([]string{}, args...)
		return 0, false
	}

	if code := cmdRun([]string{"ehti", "-p", "hello"}); code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	want := []string{"-p", "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("launch args = %v, want %v", got, want)
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
