package ptyauth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func buildFakeClaude(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	src := filepath.Join(wd, "..", "..", "testdata", "fakeclaude")
	name := "fakeclaude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)

	cmd := exec.Command("go", "build", "-o", out, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fakeclaude: %v\n%s", err, output)
	}
	return out
}

func testConfig(t *testing.T, fake, configDir string, extraEnv ...string) Config {
	t.Helper()
	return Config{
		ClaudeBinary: fake,
		ConfigDir:    configDir,
		Env:          append(append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir), extraEnv...),
		Timeout:      20 * time.Second,
		PollInterval: 200 * time.Millisecond,
		WorkingDir:   configDir,
	}
}

func collect(t *testing.T, sess *Session) []Event {
	t.Helper()
	var events []Event
	for ev := range sess.Events() {
		events = append(events, ev)
	}
	return events
}

func TestLoginEmitsURLThenLinked(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	sess, err := Start(context.Background(), testConfig(t, fake, configDir))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	var sawURL, sawLinked bool
	var gotURL string
	for _, ev := range collect(t, sess) {
		switch ev.Type {
		case EventURL:
			sawURL, gotURL = true, ev.URL
		case EventLinked:
			sawLinked = true
		case EventFailed, EventTimeout:
			t.Fatalf("unexpected event: %+v", ev)
		}
	}

	if !sawURL {
		t.Fatal("expected an EventURL")
	}
	if !sawLinked {
		t.Error("expected an EventLinked")
	}

	// The whole URL, not the part that fits on one terminal line. This
	// is the regression guard for the pseudo-terminal being too narrow:
	// the real OAuth URL is ~600 characters and used to arrive
	// truncated at the wrap point, which is a link that simply fails.
	if len(gotURL) < 400 {
		t.Errorf("URL looks truncated (%d chars): %s", len(gotURL), gotURL)
	}
	if !hasSuffixState(gotURL) {
		t.Errorf("URL is missing its trailing state parameter: %s", gotURL)
	}
}

func hasSuffixState(url string) bool {
	const tail = "ZYXWVUTSRQPONMLKJIHGFEDCBA"
	return len(url) >= len(tail) && url[len(url)-len(tail):] == tail
}

// TestLoginCompletesViaPastedCode covers the flow where the browser
// hands the user a code to paste back rather than redirecting to a
// local callback — the real `claude auth login` prints
// "Paste code here if prompted >" and waits.
func TestLoginCompletesViaPastedCode(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	sess, err := Start(context.Background(), testConfig(t, fake, configDir, "FAKECLAUDE_REQUIRE_CODE=1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	events := make(chan Event, 8)
	go func() {
		for ev := range sess.Events() {
			events <- ev
		}
		close(events)
	}()

	// Wait for the URL, then answer the way the web UI would.
	var linked bool
	for ev := range events {
		if ev.Type == EventURL {
			if err := sess.SubmitCode("test-auth-code"); err != nil {
				t.Fatalf("SubmitCode: %v", err)
			}
		}
		if ev.Type == EventLinked {
			linked = true
		}
		if ev.Type == EventFailed || ev.Type == EventTimeout {
			t.Fatalf("unexpected event: %+v", ev)
		}
	}
	if !linked {
		t.Error("expected the pasted code to complete the login")
	}
}

func TestLoginTimesOutIfNeverCompleted(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	cfg := testConfig(t, fake, configDir, "FAKECLAUDE_NEVER_COMPLETE=1")
	cfg.Timeout = 2 * time.Second

	sess, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	var last Event
	for _, ev := range collect(t, sess) {
		last = ev
	}
	if last.Type != EventTimeout && last.Type != EventFailed {
		t.Errorf("last event = %+v, want timeout or failed", last)
	}
}

func TestSubmitCodeRejectsEmpty(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	sess, err := Start(context.Background(), testConfig(t, fake, configDir, "FAKECLAUDE_REQUIRE_CODE=1"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	if err := sess.SubmitCode("   "); err == nil {
		t.Error("expected an error for an empty code")
	}
}
