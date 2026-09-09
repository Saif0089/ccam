//go:build windows

package updater

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unicode/utf16"
)

// stubElevation replaces the elevated path for the duration of a test and
// reports whether it was reached. No test may ever raise a real UAC
// prompt: CI runs unattended and a prompt would hang until the job's
// timeout, which is exactly the failure this whole design avoids.
func stubElevation(t *testing.T, err error) *bool {
	t.Helper()
	called := false
	previous := elevatedReplace
	elevatedReplace = func(from, to string) error {
		called = true
		return err
	}
	t.Cleanup(func() { elevatedReplace = previous })
	return &called
}

// TestReplaceBinaryNeverElevatesOnAnOrdinaryInstall is the property that
// keeps every normal machine — and CI — prompt-free. ccam installs under
// %LOCALAPPDATA%, which the user owns, so the unprivileged rename works
// and the elevated path must not be reached at all.
func TestReplaceBinaryNeverElevatesOnAnOrdinaryInstall(t *testing.T) {
	called := stubElevation(t, errors.New("must not be called"))

	dir := t.TempDir()
	to := filepath.Join(dir, "ccam.exe")
	from := filepath.Join(dir, "ccam.exe.new")
	if err := os.WriteFile(to, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(from, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(from, to); err != nil {
		t.Fatalf("replaceBinary on a writable directory: %v", err)
	}
	if *called {
		t.Error("elevation was requested for a directory the user can write; " +
			"an ordinary install must never raise a UAC prompt")
	}
	if got, err := os.ReadFile(to); err != nil || string(got) != "new" {
		t.Errorf("binary content = %q (err %v), want %q", got, err, "new")
	}
}

// TestReplaceBinaryElevatesOnlyOnAccessDenied pins the trigger. Elevation
// must answer "Windows refused for want of rights" and nothing else: a
// missing file or a locked one is not fixed by administrator rights, and
// prompting for them would be noise the user has to dismiss.
func TestReplaceBinaryElevatesOnlyOnAccessDenied(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "ccam.exe.new")
	if err := os.WriteFile(from, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The target does not exist, so the first rename fails — but with
	// "not found", which elevation cannot help with.
	called := stubElevation(t, nil)
	if err := replaceBinary(from, filepath.Join(dir, "absent.exe")); err == nil {
		t.Error("replacing a binary that is not there should fail")
	}
	if *called {
		t.Error("elevation was requested for a missing file, which administrator rights cannot fix")
	}
}

func TestIsAccessDenied(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"ERROR_ACCESS_DENIED", &os.LinkError{Err: syscall.Errno(5)}, true},
		{"fs.ErrPermission", fs.ErrPermission, true},
		{"wrapped permission error", &os.PathError{Err: fs.ErrPermission}, true},
		{"not found", &os.LinkError{Err: syscall.Errno(2)}, false},
		{"sharing violation", &os.LinkError{Err: syscall.Errno(32)}, false},
		{"nil", nil, false},
		{"unrelated", errors.New("disk full"), false},
	} {
		if got := isAccessDenied(tc.err); got != tc.want {
			t.Errorf("%s: isAccessDenied(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

// TestEncodePowerShellCommand checks the wire format -EncodedCommand
// documents: UTF-16LE then base64. Getting this wrong would send the
// elevated shell garbage, and it would fail only on a machine that had
// already prompted the user.
func TestEncodePowerShellCommand(t *testing.T) {
	const script = "Write-Host 'ok'"
	decoded, err := base64.StdEncoding.DecodeString(encodePowerShellCommand(script))
	if err != nil {
		t.Fatalf("output is not base64: %v", err)
	}
	if len(decoded)%2 != 0 {
		t.Fatalf("decoded length %d is not a whole number of UTF-16 units", len(decoded))
	}
	units := make([]uint16, 0, len(decoded)/2)
	for i := 0; i < len(decoded); i += 2 {
		units = append(units, uint16(decoded[i])|uint16(decoded[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != script {
		t.Errorf("round trip = %q, want %q", got, script)
	}
}

// TestPsQuoteEscapesPathsWithQuotes covers the real-world path shapes
// that reach the elevated script — spaces are routine and an apostrophe
// in a Windows user name is legal.
func TestPsQuoteEscapesPathsWithQuotes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`C:\Users\John Smith\ccam.exe`, `'C:\Users\John Smith\ccam.exe'`},
		{`C:\Users\O'Brien\ccam.exe`, `'C:\Users\O''Brien\ccam.exe'`},
	} {
		if got := psQuote(tc.in); got != tc.want {
			t.Errorf("psQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
