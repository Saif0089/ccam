package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"clawdh/internal/switching"
)

// seedShares writes a shares cache next to the seeded ~/.clawdh, so `clawdh
// shared <slug>` resolves a gateway URL and key the way it would on a machine.
func seedShares(t *testing.T, home, json string) {
	t.Helper()
	mustWrite(t, filepath.Join(home, ".clawdh", "shares.json"), json)
}

const twoShares = `[
  {"account":"Hassan","slug":"hassan","gateway":"https://gw.example","key":"hassan-key"},
  {"account":"Ehtisham","slug":"ehtisham","gateway":"https://gw.example","key":"ehtisham-key"}
]`

// Inside a supervised session, `clawdh shared <slug>` is a switch: it stages a
// handoff marked shared and lets the supervisor relaunch — it does not try to
// start a second interactive session.
func TestSharedStagesSwitchInsideSupervisedSession(t *testing.T) {
	home := seedRunEnv(t)
	seedShares(t, home, twoShares)

	handoff := filepath.Join(t.TempDir(), "handoff.json")
	t.Setenv(switching.HandoffEnvVar, handoff)
	t.Setenv(switching.SessionIDEnvVar, "sess-x")
	t.Setenv(switching.SupervisorEnvVar, strconv.Itoa(os.Getpid())) // an alive supervisor

	if code := cmdShared([]string{"ehtisham"}); code != 0 {
		t.Fatalf("cmdShared exit = %d, want 0 (staged, not launched)", code)
	}
	h, ok := switching.ReadHandoff(handoff)
	if !ok {
		t.Fatal("no handoff was staged")
	}
	if h.Account != "ehtisham" || !h.Shared || h.SessionID != "sess-x" {
		t.Errorf("handoff = %+v, want {Account:ehtisham, Shared:true, SessionID:sess-x}", h)
	}
}

// `clawdh shared X -p "…"` inside a session is a deliberate one-shot on the
// share, not a switch, so it must not stage a handoff.
func TestSharedWithArgsInsideASessionDoesNotStage(t *testing.T) {
	home := seedRunEnv(t)
	seedShares(t, home, twoShares)

	handoff := filepath.Join(t.TempDir(), "handoff.json")
	t.Setenv(switching.HandoffEnvVar, handoff)
	t.Setenv(switching.SupervisorEnvVar, strconv.Itoa(os.Getpid()))

	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(_ string, _, _ []string, _, _ string, _ onSwitch) (int, bool) { return 0, false }

	if code := cmdShared([]string{"ehtisham", "-p", "hi"}); code != 0 {
		t.Fatalf("cmdShared -p exit = %d, want 0", code)
	}
	if _, ok := switching.ReadHandoff(handoff); ok {
		t.Error("a one-shot `clawdh shared X -p` staged a switch; it must run in place")
	}
}

// The supervisor relaunches a shared session onto another share when a
// shared-marked handoff is staged, carrying that share's gateway URL and key.
func TestSharedSupervisorRelaunchesOntoAnotherShare(t *testing.T) {
	home := seedRunEnv(t)
	seedShares(t, home, twoShares)
	seedTranscript(t, home, "sess-9")

	var envs [][]string
	calls := 0
	origRunner := claudeRunner
	t.Cleanup(func() { claudeRunner = origRunner })
	claudeRunner = func(_ string, _, env []string, handoff, _ string, _ onSwitch) (int, bool) {
		envs = append(envs, env)
		calls++
		if calls == 1 {
			// Simulate switching from the hassan share to the ehtisham share.
			if err := switching.WriteHandoff(handoff, switching.Handoff{Account: "ehtisham", SessionID: "sess-9", Shared: true}); err != nil {
				t.Fatal(err)
			}
			return 0, true
		}
		return 0, false
	}

	if code := cmdShared([]string{"hassan"}); code != 0 {
		t.Fatalf("cmdShared exit = %d, want 0", code)
	}
	if calls != 2 {
		t.Fatalf("want 2 launches (hassan, then ehtisham), got %d", calls)
	}
	// The first launch is on hassan's key; the second on ehtisham's, both pointed
	// at the gateway — the switch actually moved the account.
	if !hasEnv(envs[0], "ANTHROPIC_AUTH_TOKEN=hassan-key") {
		t.Errorf("first launch not on hassan's key: %v", authVars(envs[0]))
	}
	if !hasEnv(envs[1], "ANTHROPIC_AUTH_TOKEN=ehtisham-key") {
		t.Errorf("relaunch not on ehtisham's key: %v", authVars(envs[1]))
	}
	if !hasEnv(envs[1], "ANTHROPIC_BASE_URL=https://gw.example") {
		t.Errorf("relaunch not pointed at the gateway: %v", authVars(envs[1]))
	}
	// And the supervisor never leaks a competing API key into the session.
	for _, e := range envs[1] {
		if strings.HasPrefix(strings.ToUpper(e), "ANTHROPIC_API_KEY=") {
			t.Error("ANTHROPIC_API_KEY leaked into a shared session")
		}
	}
}

func hasEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

func authVars(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "ANTHROPIC_") {
			out = append(out, e)
		}
	}
	return out
}
