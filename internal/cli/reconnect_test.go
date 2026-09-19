package cli

import (
	"path/filepath"
	"testing"
	"time"

	"clawdh/internal/switching"
	"clawdh/panel"
)

func TestSessionIDFromArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--session-id", "abc"}, "abc"},
		{[]string{"--session-id=xyz"}, "xyz"},
		{[]string{"--resume", "r1"}, "r1"},
		{[]string{"--resume=r2"}, "r2"},
		{[]string{"-r", "r3"}, "r3"},
		{[]string{"--continue"}, ""}, // names no id
		{[]string{"foo", "bar"}, ""},
		{nil, ""},
	} {
		if got := sessionIDFromArgs(tc.args); got != tc.want {
			t.Errorf("sessionIDFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// A shared session watches its gateway key: it stages nothing while the key is
// unchanged, and when a revoke+re-grant replaces it, it stages the ordinary
// switch back to the same account — carrying the session id so the conversation
// resumes — which the supervisor relaunches with the fresh key.
func TestWatchShareKey(t *testing.T) {
	dir := t.TempDir()
	sharesPath := filepath.Join(dir, "shares.json")
	handoff := filepath.Join(dir, "handoff.json")
	save := func(key string) {
		if err := panel.SaveShares(sharesPath, []panel.GatewayShare{
			{Account: "Ehtisham", Slug: "ehtisham", Gateway: "https://gw", Key: key},
		}); err != nil {
			t.Fatal(err)
		}
	}
	save("K1")

	orig := shareKeyPollInterval
	shareKeyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { shareKeyPollInterval = orig })

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		watchShareKey(sharesPath, "ehtisham", "K1", "sess-1", handoff, stop)
		close(done)
	}()

	// The key is unchanged: the watcher stages nothing.
	time.Sleep(30 * time.Millisecond)
	if _, ok := switching.ReadHandoff(handoff); ok {
		t.Fatal("watcher staged a handoff while the key was unchanged")
	}

	// A re-grant mints a new key; the watcher should stage the switch and return.
	save("K2")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(stop)
		t.Fatal("watcher did not stage a switch after the key changed")
	}

	h, ok := switching.ReadHandoff(handoff)
	if !ok || h.Account != "ehtisham" || !h.Shared || h.SessionID != "sess-1" {
		t.Errorf("staged handoff = %+v (ok=%v), want ehtisham / shared / sess-1", h, ok)
	}
}
