package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// Generate returns a random URL-safe hex token suitable for enrollment
// tokens and API keys.
func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Hash returns a deterministic SHA-256 hex digest of a token, used so API
// keys can be looked up by hash without ever storing them in plaintext.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
