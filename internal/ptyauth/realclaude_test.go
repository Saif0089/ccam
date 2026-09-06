package ptyauth

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccam/internal/ptyio"

	"github.com/hinshun/vt10x"
)

// TestRealClaudeLoginScreens is a manual probe, skipped by default: it
// spawns the REAL `claude` CLI against a throwaway CLAUDE_CONFIG_DIR and
// dumps the rendered terminal screen every second, so a human can see
// exactly what its first-run/login flow puts on screen (and therefore
// what ptyauth has to recognise and respond to). CI can't run this —
// there's no real Anthropic account there — but every assumption in
// session.go about `claude`'s TUI ultimately traces back to what this
// prints.
//
//	CCAM_REAL_CLAUDE=1 go test ./internal/ptyauth/ -run TestRealClaudeLoginScreens -v
//
// It never completes a login and sends no keystrokes; it only watches.
func TestRealClaudeLoginScreens(t *testing.T) {
	if os.Getenv("CCAM_REAL_CLAUDE") == "" {
		t.Skip("manual probe; set CCAM_REAL_CLAUDE=1 to run against the real claude CLI")
	}

	binary := os.Getenv("CCAM_CLAUDE_BIN")
	if binary == "" {
		binary = "claude"
	}
	var args []string
	if raw := os.Getenv("CCAM_CLAUDE_ARGS"); raw != "" {
		args = strings.Fields(raw)
	}
	configDir := t.TempDir()
	t.Logf("spawning %s %v with CLAUDE_CONFIG_DIR=%s", binary, args, configDir)

	home, _ := os.UserHomeDir()
	sess, err := ptyio.Start(binary, args, append(os.Environ(),
		"CLAUDE_CONFIG_DIR="+configDir,
	), home)
	if err != nil {
		t.Fatalf("starting %s: %v", binary, err)
	}
	defer sess.Close()

	term := vt10x.New(vt10x.WithSize(120, 40))
	screens := make(chan string, 64)
	go func() {
		defer close(screens)
		reader := bufio.NewReader(sess)
		for {
			if err := term.Parse(reader); err != nil {
				return
			}
			screens <- term.String()
		}
	}()

	watchFor := 20 * time.Second
	deadline := time.After(watchFor)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	latest := ""
	for done := false; !done; {
		select {
		case s, ok := <-screens:
			if !ok {
				t.Logf("=== claude's output stream ended ===")
				done = true
				continue
			}
			latest = s
		case <-tick.C:
			t.Logf("=== screen at %s ===\n%s", time.Now().Format("15:04:05"), latest)
			if url := urlPattern.FindString(latest); url != "" {
				t.Logf(">>> urlPattern MATCHED: %s", url)
			} else {
				t.Logf(">>> urlPattern found no URL yet")
			}
		case <-deadline:
			done = true
		}
	}

	t.Logf("=== final screen ===\n%s", latest)
	if entries, err := os.ReadDir(configDir); err == nil {
		for _, e := range entries {
			t.Logf("config dir now contains: %s", filepath.Join(configDir, e.Name()))
		}
	}
}
