package ptyio

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestStartCapturesChildOutput(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	sess, err := Start(fake, []string{"auth", "login", "--claudeai"},
		append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir), configDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(string(buf), "Login successful") {
		n, err := sess.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
	}

	if !strings.Contains(string(buf), "fake-auth.example.com") {
		t.Errorf("expected auth URL in output, got %q", buf)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := sess.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	if _, err := os.Stat(filepath.Join(configDir, ".credentials.json")); err != nil {
		t.Errorf("expected credentials file to be written: %v", err)
	}
}
