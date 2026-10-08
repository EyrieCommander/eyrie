package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// NewToken returns a random bridge token and its hex SHA-256.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = "eyb_" + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is the stored form of a bridge token.
func HashToken(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

// tokenMatches compares SHA-256(presented) to the stored hash in constant time.
func tokenMatches(presented, storedHex string) bool {
	want, err := hex.DecodeString(storedHex)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// RotateToken generates a new token, stores only its hash in the config
// file at path (creating it 0600 if needed), and returns the token so the
// caller can print it once. Other keys in the file are preserved.
func RotateToken(path string) (string, error) {
	c, err := ReadConfig(path)
	if err != nil && err != ErrNotConfigured {
		return "", err
	}
	token, hash, err := NewToken()
	if err != nil {
		return "", err
	}
	c.BridgeTokenSHA256 = hash
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("# Eyrie bridge secret config. Keep this file 0600 and out of any repo.\n")
	if err := toml.NewEncoder(&sb).Encode(c); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("save %s: %w", path, err)
	}
	return token, nil
}
