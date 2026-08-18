package session

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomID returns a 64-bit random identifier (8 bytes, hex-encoded) suitable
// for short-lived, non-cryptographic identifiers such as async operation
// handles and CSRF tokens.
func RandomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StrongRandomID returns a 128-bit random identifier (16 bytes, hex-encoded).
// It backs one-time hand-off URLs that guard high-value secrets (a vault
// recovery mnemonic), where 64-bit entropy in RandomID is too guessable on an
// otherwise unauthenticated route.
func StrongRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
