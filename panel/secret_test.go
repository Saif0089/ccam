package panel

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSealedLoginsCannotBeReadWithoutTheKey(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadSecret(filepath.Join(dir, "panel.key"))
	if err != nil {
		t.Fatal(err)
	}
	login := []byte(`{"claudeAiOauth":{"accessToken":"secret"}}`)
	sealed, err := s.Seal(login)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "secret") {
		t.Fatal("the sealed login still contains the token in the clear")
	}
	back, err := s.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(login) {
		t.Errorf("round trip = %q, want %q", back, login)
	}

	// A different panel, a different key: the file alone is not enough.
	other, err := LoadSecret(filepath.Join(t.TempDir(), "panel.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed); err == nil {
		t.Error("another panel's key opened this login")
	}
}

func TestTheKeyIsReusedNotRegenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.key")
	first, err := LoadSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := first.Seal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	// A restart must be able to read what the last run wrote.
	second, err := LoadSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	back, err := second.Open(sealed)
	if err != nil || string(back) != "hello" {
		t.Errorf("after a restart: %q, %v", back, err)
	}
}

func TestPasswordVerification(t *testing.T) {
	admin, err := SetPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !admin.Verify("correct horse battery staple") {
		t.Error("the right password was rejected")
	}
	if admin.Verify("correct horse battery stapl") {
		t.Error("a wrong password was accepted")
	}
	// The password itself is nowhere in what gets stored.
	if strings.Contains(string(admin.Hash)+string(admin.Salt), "horse") {
		t.Error("the stored admin record contains the password")
	}
}

func TestTokensAreStoredOnlyAsHashes(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == hash {
		t.Fatal("the token is being stored as itself")
	}
	if !SameToken(HashToken(token), hash) {
		t.Error("a freshly minted token does not match its own hash")
	}
	if SameToken(HashToken(token+"x"), hash) {
		t.Error("a different token matched")
	}
}
