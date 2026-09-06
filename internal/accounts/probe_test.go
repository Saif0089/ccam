package accounts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildFakeClaude compiles testdata/fakeclaude and returns its path.
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

func TestIsLinkedFalseWhenNotAuthenticated(t *testing.T) {
	p := &Prober{ClaudeBinary: buildFakeClaude(t)}
	if p.IsLinked(context.Background(), t.TempDir()) {
		t.Error("expected IsLinked = false for a fresh config dir")
	}
}

func TestIsLinkedTrueWhenAuthenticated(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()
	writeFakeCredentials(t, configDir)

	p := &Prober{ClaudeBinary: fake}
	if !p.IsLinked(context.Background(), configDir) {
		t.Error("expected IsLinked = true once the account is authenticated")
	}
}

// TestIsLinkedIgnoresCredentialFileWithoutClaudeAgreeing guards the
// reason this probe asks the CLI at all: on macOS the credentials can
// live in the Keychain with no file on disk, so file presence is not
// the question — what `claude auth status` says is.
func TestIsLinkedAsksTheCLINotTheFilesystem(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()

	// A file that exists but is empty must not read as authenticated.
	if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), nil, 0o600); err != nil {
		t.Fatalf("writing empty credentials: %v", err)
	}

	p := &Prober{ClaudeBinary: fake}
	if p.IsLinked(context.Background(), configDir) {
		t.Error("expected IsLinked = false when claude reports loggedIn:false")
	}
}

func TestStatusReportsAuthMethod(t *testing.T) {
	fake := buildFakeClaude(t)
	configDir := t.TempDir()
	writeFakeCredentials(t, configDir)

	p := &Prober{ClaudeBinary: fake}
	status, err := p.Status(context.Background(), configDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.LoggedIn {
		t.Errorf("LoggedIn = false, want true")
	}
	if status.AuthMethod != "claude.ai" {
		t.Errorf("AuthMethod = %q, want claude.ai", status.AuthMethod)
	}
}

func TestIsLinkedFalseWhenClaudeMissing(t *testing.T) {
	p := &Prober{ClaudeBinary: filepath.Join(t.TempDir(), "does-not-exist")}
	if p.IsLinked(context.Background(), t.TempDir()) {
		t.Error("expected IsLinked = false when the claude binary can't be run")
	}
}

func writeFakeCredentials(t *testing.T, configDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), []byte(`{"fake":true}`), 0o600); err != nil {
		t.Fatalf("writing fake credentials: %v", err)
	}
}
