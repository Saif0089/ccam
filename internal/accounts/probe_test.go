package accounts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildFakeClaude compiles testdata/fakeclaude once per test process and
// returns the path to the resulting binary.
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

func TestProberIsLinkedFalseWhenNoCredentials(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	p := &Prober{ClaudeBinary: fake}
	if p.IsLinked(context.Background(), configDir) {
		t.Error("expected IsLinked = false for an empty config dir")
	}
}

func TestProberIsLinkedTrueAfterFileAppears(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), []byte(`{"fake":true}`), 0o600); err != nil {
		t.Fatalf("writing fake credentials: %v", err)
	}

	p := &Prober{ClaudeBinary: fake}
	if !p.IsLinked(context.Background(), configDir) {
		t.Error("expected IsLinked = true once a credentials file exists")
	}
}

func TestProberIsLinkedTrueViaHeadlessProbe(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	// No credentials file, but the fake binary's own headless-probe mode
	// is stubbed to succeed when we tell it credentials exist some other
	// way (e.g. a real backend's Keychain). Simulate that here by writing
	// the file fakeclaude checks, then deleting it only from the fast
	// path's perspective is impossible in this harness, so instead this
	// test just confirms the fallback path is reached and agrees with the
	// fast path for a linked account.
	if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), []byte(`{"fake":true}`), 0o600); err != nil {
		t.Fatalf("writing fake credentials: %v", err)
	}

	p := &Prober{ClaudeBinary: fake}
	if !p.headlessProbeSucceeds(context.Background(), configDir) {
		t.Error("expected headless probe to succeed once credentials exist")
	}
}
