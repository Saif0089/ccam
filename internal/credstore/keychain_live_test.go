package credstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Deliberately touches the real keychain, with a scratch directory no account
// uses. Run explicitly: go test ./internal/credstore -run LiveKeychain -v
func TestLiveKeychainRoundTrip(t *testing.T) {
	if os.Getenv("CCAM_LIVE_KEYCHAIN") != "1" {
		t.Skip("set CCAM_LIVE_KEYCHAIN=1 to run against the real keychain")
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".ccam", "probe-live")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Delete(dir); os.RemoveAll(dir) })

	for _, pad := range []int{0, 4096, 8192} {
		payload, _ := json.Marshal(map[string]any{
			"claudeAiOauth": map[string]any{"accessToken": "not-a-real-token"},
			"mcpOAuth":      map[string]any{"pad": strings.Repeat("p", pad)},
		})
		if err := Write(dir, payload); err != nil {
			t.Fatalf("pad=%d write: %v", pad, err)
		}
		got, err := Read(dir)
		if err != nil {
			t.Fatalf("pad=%d read: %v", pad, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("pad=%d: read back %d bytes, wrote %d — CORRUPT", pad, len(got), len(payload))
		}
		kc := KeychainBacked(dir)
		t.Logf("pad=%-5d payload=%5dB  round-trip OK  backend=%s", pad, len(payload),
			map[bool]string{true: "keychain", false: "file"}[kc])
	}
}
