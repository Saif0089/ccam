package credstore

import "testing"

// Valid is what stops the wreckage of a bad write being read back as a login.
// The exact shape below is what a truncated `security -i` write left in this
// machine's keychain: hex text, because the fragment was not valid UTF-8 and
// `security -w` printed it as hex, which ccam then stored and hex-encoded
// again.
func TestValidAndHasLogin(t *testing.T) {
	cases := []struct {
		name            string
		data            string
		valid, hasLogin bool
	}{
		{"a real store", `{"claudeAiOauth":{"accessToken":"not-a-real-token"},"mcpOAuth":{}}`, true, true},
		{"logged out", `{"mcpOAuth":{}}`, true, false},
		{"empty token", `{"claudeAiOauth":{"accessToken":""}}`, true, false},
		{"hex text from a truncated write", `07226d63704f41757468223a7b22`, false, false},
		{"truncated json", `{"claudeAiOauth":{"accessToken":"not-a-`, false, false},
		{"nothing at all", ``, false, false},
		{"a json array", `["claudeAiOauth"]`, false, false},
	}
	for _, c := range cases {
		if got := Valid([]byte(c.data)); got != c.valid {
			t.Errorf("%s: Valid = %v, want %v", c.name, got, c.valid)
		}
		if got := HasLogin([]byte(c.data)); got != c.hasLogin {
			t.Errorf("%s: HasLogin = %v, want %v", c.name, got, c.hasLogin)
		}
	}
}
