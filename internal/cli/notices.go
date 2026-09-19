package cli

import (
	"encoding/json"
	"os"
	"time"

	"clawdh/internal/config"
	"clawdh/internal/notify"
	"clawdh/panel"
)

// Desktop notifications for the person at a machine. clawdh runs as a background
// service, so the moments it has something to say — an account was shared with
// you or taken back, this machine was asked to do something, you're near a quota,
// a login you rely on broke — have nowhere to appear but a notification.
//
// Every notification is best-effort (a machine with no notifier, or with them
// turned off, must never cost more than a skipped call) and short: a one-line
// body under the "clawdh" title, so it reads at a glance.

// noticeMemory is how long a shown notice's ID is remembered, so a standing
// condition (a quota over its mark all week) is announced once, while the same
// kind of event recurring later — a new window, a fresh breakage — still is.
const noticeMemory = 8 * 24 * time.Hour

// notifyBrief shows one short desktop notification, best-effort.
func notifyBrief(body string) {
	if !notify.Enabled() {
		return
	}
	_ = notify.Send(body)
}

// showNotices surfaces the panel's per-person notices from a check-in, each at
// most once, using a small on-disk set of IDs already shown so a standing
// condition that rides every check-in is announced a single time.
func showNotices(notices []panel.Notice) {
	if len(notices) == 0 {
		return
	}
	path, err := config.NoticesSeenFile()
	if err != nil {
		// No home dir to remember against: better to show them (possibly again)
		// than to swallow a real warning.
		for _, n := range notices {
			notifyBrief(n.Body)
		}
		return
	}
	show, next := selectUnseen(notices, loadSeen(path), time.Now().Unix())
	for _, n := range show {
		notifyBrief(n.Body)
	}
	saveSeen(path, next)
}

// selectUnseen picks the notices whose ID isn't already remembered, and returns
// the set to remember next: every current ID stamped now, plus prior IDs still
// within noticeMemory so the memory stays bounded. Pure, for tests.
func selectUnseen(notices []panel.Notice, seen map[string]int64, now int64) (show []panel.Notice, next map[string]int64) {
	next = map[string]int64{}
	cutoff := now - int64(noticeMemory/time.Second)
	for id, ts := range seen {
		if ts >= cutoff {
			next[id] = ts
		}
	}
	for _, n := range notices {
		if _, ok := seen[n.ID]; !ok {
			show = append(show, n)
		}
		next[n.ID] = now
	}
	return show, next
}

func loadSeen(path string) map[string]int64 {
	seen := map[string]int64{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return seen
	}
	_ = json.Unmarshal(raw, &seen)
	return seen
}

func saveSeen(path string, seen map[string]int64) {
	if raw, err := json.Marshal(seen); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}
