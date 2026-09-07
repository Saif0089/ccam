//go:build windows

package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildCcamForStartup compiles cmd/ccam for the autostart test.
func buildCcamForStartup(t *testing.T, dir string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	out := filepath.Join(dir, "ccam.exe")
	cmd := exec.Command("go", "build", "-o", out, filepath.Join(wd, "..", "..", "cmd", "ccam"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building ccam: %v\n%s", err, output)
	}
	return out
}

// TestStartupScriptActuallyStartsTheService runs the Startup-folder
// script the way Windows does at logon, and checks a ccam server comes
// up.
//
// The e2e test covers `ccam install`, but nothing else ever executes
// the artifact that install *writes* — so a .cmd that Windows would
// choke on (an unescaped % mangling the PATH line, a quote closing the
// set statement early) would look installed and simply never start
// anything at the next logon, silently.
func TestStartupScriptActuallyStartsTheService(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	// A PATH carrying the characters that break a naive `set "PATH=..."`.
	t.Setenv("PATH", `C:\odd 100%dir;`+os.Getenv("PATH"))

	binary := buildCcamForStartup(t, home)
	const port = 47987
	svc := New(binary, port)

	artifact, err := svc.Install(binary, port)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	t.Logf("autostart artifact: %s", artifact)
	t.Cleanup(func() {
		_ = svc.Stop()
		_ = svc.Uninstall()
	})

	if !strings.HasSuffix(artifact, ".cmd") {
		t.Skipf("Startup folder was not writable; fell back to %s", artifact)
	}

	script, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("reading startup script: %v", err)
	}
	t.Logf("startup script:\n%s", script)

	// cmd.exe expands %VAR% as it parses the line, so a literal % has
	// to arrive doubled or the PATH silently loses entries.
	if strings.Contains(string(script), "100%dir") {
		t.Errorf("a bare %% survived into the .cmd; cmd.exe would mangle the PATH:\n%s", script)
	}

	// Stop whatever Install started, so what follows is attributable to
	// the script alone.
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop before running the script: %v", err)
	}

	// Now run it exactly as Windows would at logon.
	run := exec.Command("cmd.exe", "/c", artifact)
	if err := run.Run(); err != nil {
		t.Fatalf("running the startup script: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if info, _ := Running(); info != nil {
			t.Logf("service came up on port %d via the startup script", info.Port)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}

	log, _ := os.ReadFile(filepath.Join(home, ".ccam", "ccam.log"))
	t.Fatalf("the startup script did not bring up the service\nccam log:\n%s", log)
}

// TestInstallClearsAScheduledTaskFallback guards against both autostart
// mechanisms being registered at once, which would start two ccams at
// logon and leave one failing to bind the port.
func TestInstallClearsAScheduledTaskFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))

	binary := filepath.Join(home, "ccam.exe")
	if err := os.WriteFile(binary, []byte("stub"), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}

	svc := &windowsService{generic{binaryPath: binary, port: 47988}}
	if _, err := svc.Install(binary, 47988); err != nil {
		t.Fatalf("Install: %v", err)
	}
	t.Cleanup(func() { _ = svc.Uninstall() })

	// No scheduled task should be left registered when the Startup
	// script is the mechanism in use.
	if err := exec.Command("schtasks", "/Query", "/TN", "ccam").Run(); err == nil {
		t.Error("a scheduled task is registered alongside the Startup script")
	}
}
