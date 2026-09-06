package ptyauth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	out := filepath.Join(t.TempDir(), "fakeclaude")

	cmd := exec.Command("go", "build", "-o", out, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fakeclaude: %v\n%s", err, output)
	}
	return out
}

func TestRunEmitsURLThenLinked(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	cfg := Config{
		ClaudeBinary: fake,
		ConfigDir:    configDir,
		Env:          append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir),
		Timeout:      10 * time.Second,
		PollInterval: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var sawURL, sawLinked bool
	var gotURL string
	for ev := range Run(ctx, cfg) {
		switch ev.Type {
		case EventURL:
			sawURL = true
			gotURL = ev.URL
		case EventLinked:
			sawLinked = true
		case EventFailed, EventTimeout:
			t.Fatalf("unexpected event: %+v", ev)
		}
	}

	if !sawURL {
		t.Error("expected an EventURL")
	}
	if gotURL != "https://fake-auth.example.com/oauth?code=demo-12345" {
		t.Errorf("URL = %q, unexpected", gotURL)
	}
	if !sawLinked {
		t.Error("expected an EventLinked")
	}
}

func TestRunTimesOutIfNeverLinked(t *testing.T) {
	// A binary that never writes credentials (here: a shell "true" via a
	// tiny helper) should end in EventTimeout, not hang.
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	cfg := Config{
		ClaudeBinary: fake,
		ConfigDir:    configDir,
		Env:          append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir, "FAKECLAUDE_NEVER_COMPLETE=1"),
		Timeout:      1 * time.Second,
		PollInterval: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var last Event
	for ev := range Run(ctx, cfg) {
		last = ev
	}
	if last.Type != EventTimeout && last.Type != EventFailed {
		t.Errorf("last event = %+v, want timeout or failed", last)
	}
}
