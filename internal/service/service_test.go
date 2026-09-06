package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildCcam compiles cmd/ccam once for this test process.
func buildCcam(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	src := filepath.Join(wd, "..", "..", "cmd", "ccam")
	name := "ccam"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)

	cmd := exec.Command("go", "build", "-o", out, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building ccam: %v\n%s", err, output)
	}
	return out
}

// withFakeHome points $HOME (and, on Windows, the vars UserHomeDir/our
// own path helpers consult) at a fresh temp dir for the duration of the
// test, so Install/Start/Stop/Uninstall never touch the real machine's
// actual LaunchAgents/systemd-user/Startup folder.
func withFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	restore := map[string]string{}
	set := func(key, val string) {
		restore[key] = os.Getenv(key)
		os.Setenv(key, val)
	}
	set("HOME", home)
	if runtime.GOOS == "windows" {
		set("USERPROFILE", home)
		set("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		set("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
	t.Cleanup(func() {
		for k, v := range restore {
			os.Setenv(k, v)
		}
	})
	return home
}

func TestInstallStartStopUninstallLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real background process; skipped in -short")
	}
	binary := buildCcam(t)
	home := withFakeHome(t)

	const port = 47999
	svc := New(binary, port)

	if installed, err := svc.IsInstalled(); err != nil || installed {
		t.Fatalf("IsInstalled before Install = %v, %v; want false, nil", installed, err)
	}

	artifact, err := svc.Install(binary, port)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if artifact == "" {
		t.Error("expected a non-empty artifact path")
	}
	if installed, err := svc.IsInstalled(); err != nil || !installed {
		t.Fatalf("IsInstalled after Install = %v, %v; want true, nil", installed, err)
	}

	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// A generous window: a freshly built, unsigned binary's first
	// execution on macOS CI runners can eat several seconds in
	// Gatekeeper's syspolicyd check alone, on top of whatever the OS
	// service manager itself needs.
	if !waitUntil(30*time.Second, func() bool {
		running, _ := svc.IsRunning()
		return running
	}) {
		t.Fatalf("service never came up after Start\n%s", diagnostics(home))
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !waitUntil(30*time.Second, func() bool {
		running, _ := svc.IsRunning()
		return !running
	}) {
		t.Fatalf("service still reports running after Stop\n%s", diagnostics(home))
	}

	if err := svc.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if installed, err := svc.IsInstalled(); err != nil || installed {
		t.Fatalf("IsInstalled after Uninstall = %v, %v; want false, nil", installed, err)
	}
}

// diagnostics dumps ccam's own log/pid/port files for a failed test's
// error message, since the failure otherwise gives no clue whether the
// process never started, started and crashed, or started but never
// bound/answered.
func diagnostics(home string) string {
	var b strings.Builder
	dump := func(label, path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(&b, "--- %s (%s): %v ---\n", label, path, err)
			return
		}
		fmt.Fprintf(&b, "--- %s (%s) ---\n%s\n", label, path, data)
	}
	dump("ccam.log", filepath.Join(home, ".ccam", "ccam.log"))
	dump("pidfile", filepath.Join(home, ".ccam", "ccam.pid"))
	dump("port file", filepath.Join(home, ".ccam", "port"))
	return b.String()
}

func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
