package credstore

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// A real Claude Code store runs to about 5 KB once MCP logins are in it, and
// `security -i` reads only 4095 bytes of one line. Over that, the truncated
// line still parses as a valid add-generic-password, so the item is REPLACED
// with a fragment of the credentials — which ccam then read back as a login and
// mirrored over the account's own store. Both accounts on the machine lost
// their logins that way. So the command has to report that it does not fit,
// rather than be sent and half-applied.
func TestKeychainCommandRefusesWhatSecurityWouldTruncate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")

	small := []byte(`{"claudeAiOauth":{"accessToken":"not-a-real-token"}}`)
	cmd, ok := keychainAddCommand(dir, small)
	if !ok {
		t.Fatalf("a %d-byte store should fit one command line", len(small))
	}
	if len(cmd) > maxSecurityCommandLine {
		t.Fatalf("command is %d bytes, over the %d `security -i` reads", len(cmd), maxSecurityCommandLine)
	}

	big, err := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{"accessToken": "not-a-real-token"},
		"mcpOAuth":      map[string]any{"pad": strings.Repeat("p", 4096)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := keychainAddCommand(dir, big); ok {
		t.Fatalf("a %d-byte store does not fit one `security -i` line and must be refused", len(big))
	}
}

// Credentials too large for the keychain still have to be stored, and read back
// exactly: the plaintext file is Claude Code's own fallback for the same store.
func TestOversizedCredentialsRoundTripThroughTheFile(t *testing.T) {
	fileStore(t)
	dir := filepath.Join(t.TempDir(), "store")
	big, err := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{"accessToken": "not-a-real-token"},
		"mcpOAuth":      map[string]any{"pad": strings.Repeat("p", 8192)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, big); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("read back %d bytes, wrote %d", len(got), len(big))
	}
}
