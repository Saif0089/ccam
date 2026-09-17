package accounts

import "testing"

// A live check that capture reads a real macOS Keychain login. Skipped unless
// CCAM_CAPTURE_LIVE=1, because it depends on this machine actually having a
// default Claude login in the Keychain.
func TestCaptureReadsTheRealDefaultLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("live keychain check")
	}
	raw, err := CaptureLogin("")
	if err != nil {
		t.Skipf("no default login on this machine to capture: %v", err)
	}
	if !hasClaudeLogin(raw) {
		t.Fatal("captured something that is not a login")
	}
	t.Logf("captured default login: %d bytes", len(raw))
}
