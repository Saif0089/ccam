package cli

import (
	"testing"
	"time"

	"clawdh/panel"
)

// selectUnseen shows a notice ID once, keeps remembering it while it keeps
// arriving, and forgets it after noticeMemory — so a standing quota warning
// riding every check-in is announced a single time, but a genuinely new event
// later still surfaces.
func TestSelectUnseen(t *testing.T) {
	now := int64(1_700_000_000)
	n := func(id string) panel.Notice { return panel.Notice{ID: id, Body: id} }

	// First sight of two notices: both shown, both remembered.
	show, seen := selectUnseen([]panel.Notice{n("quota:95:x"), n("collision:a1:t")}, map[string]int64{}, now)
	if len(show) != 2 {
		t.Fatalf("first sight showed %d, want 2", len(show))
	}
	if _, ok := seen["quota:95:x"]; !ok {
		t.Error("a shown notice must be remembered")
	}

	// Same notices a check-in later: nothing shown again, still remembered.
	show, seen = selectUnseen([]panel.Notice{n("quota:95:x"), n("collision:a1:t")}, seen, now+30)
	if len(show) != 0 {
		t.Errorf("standing notices shown again: %+v", show)
	}

	// A new notice among the standing ones: only the new one shows.
	show, seen = selectUnseen([]panel.Notice{n("quota:95:x"), n("quota:cap:x")}, seen, now+60)
	if len(show) != 1 || show[0].ID != "quota:cap:x" {
		t.Errorf("expected only the new quota:cap notice, got %+v", show)
	}

	// After noticeMemory with the ID no longer arriving, it's forgotten — the same
	// event much later (a new window that happens to reuse a shape) shows again.
	old := seen
	_, pruned := selectUnseen(nil, old, now+60+int64(noticeMemory/time.Second)+1)
	if len(pruned) != 0 {
		t.Errorf("everything older than noticeMemory should be pruned, kept %+v", pruned)
	}
	show, _ = selectUnseen([]panel.Notice{n("quota:95:x")}, pruned, now+60+int64(noticeMemory/time.Second)+2)
	if len(show) != 1 {
		t.Error("a notice re-arriving after it was forgotten should show again")
	}
}
