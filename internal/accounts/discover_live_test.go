package accounts

import "testing"

func TestDiscoverLoginsLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live")
	}
	for _, l := range DiscoverLogins(nil) {
		src := "clawdh:" + l.ConfigDir
		if l.IsDefault {
			src = "default (~/.claude)"
		}
		t.Logf("login: %-30s plan=%-6s from %s", l.Email, l.Plan, src)
	}
}
