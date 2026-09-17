package panel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// pbkdf2Rounds stretches the admin password. The panel is reached over the
// network, so the password is the only thing between a stranger and every
// login it lends out; this is the current OWASP figure for PBKDF2-SHA256 and
// costs a fraction of a second once per sign-in.
const pbkdf2Rounds = 600_000

// Secret is the panel's own key: it seals the logins held in panel.json, so a
// copy of that file taken on its own is not a working set of credentials.
//
// This is deliberately modest. The key sits beside the file it protects, at
// 0600, which stops a backup, a stray sync client, or a careless `cat` from
// handing over real logins — it does not stop someone who already has the
// account this runs as. That is the honest boundary, and the README says so
// rather than implying more.
type Secret struct {
	aead cipher.AEAD
}

// LoadSecret reads the key at path, creating one if it is not there yet.
func LoadSecret(path string) (*Secret, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, err
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s is %d bytes, want 32: refusing to guess at a damaged key", path, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Secret{aead: aead}, nil
}

// Seal encrypts a login for storage. The nonce is prepended to the result.
func (s *Secret) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts a stored login.
func (s *Secret) Open(sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("this stored login is too short to be one")
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, errors.New("this stored login could not be read: the panel key does not match the one that wrote it")
	}
	return plain, nil
}

// SetPassword stretches a new admin password.
func SetPassword(password string) (Admin, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return Admin{}, err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Rounds, 32)
	if err != nil {
		return Admin{}, err
	}
	return Admin{Salt: salt, Hash: hash}, nil
}

// Verify reports whether password is the admin's.
func (a Admin) Verify(password string) bool {
	hash, err := pbkdf2.Key(sha256.New, password, a.Salt, pbkdf2Rounds, 32)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(hash, a.Hash) == 1
}

// NewToken mints a bearer token, returning the token to hand out and the hash
// to keep. Only the hash is stored, so the file cannot be turned back into
// working tokens.
//
// These are 32 random bytes, so unlike a password they need no stretching: a
// single hash is not a shortcut to anything.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is how a presented token is compared with a stored one.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SameToken compares in constant time, so the comparison cannot be timed to
// recover a token a byte at a time.
func SameToken(presentedHash, storedHash string) bool {
	return hmac.Equal([]byte(presentedHash), []byte(storedHash))
}

// NewJoinCode mints a short code a person types once to enrol a machine.
// Short enough to read out, and single-use with an expiry, which is what keeps
// it safe rather than its length.
func NewJoinCode() (code, hash string, err error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	code = hex.EncodeToString(b)
	return code, HashToken(code), nil
}
