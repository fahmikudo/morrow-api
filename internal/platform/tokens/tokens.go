// Package tokens creates and hashes the two bearer credential kinds.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Prefix marks a personal access token. Session tokens are bare hex, so the
// two kinds can never collide.
const Prefix = "sb_"

// Session returns 32 random bytes as 64 hex characters.
func Session() string {
	return hex.EncodeToString(random(32))
}

// TokenHash returns the SHA-256 hex digest stored for a raw credential.
func TokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// PAT returns a new personal access token: `sb_` plus 64 hex characters.
func PAT() string {
	return Prefix + hex.EncodeToString(random(32))
}

// IsPAT reports whether a raw credential is a personal access token.
func IsPAT(raw string) bool {
	return strings.HasPrefix(raw, Prefix)
}

func random(size int) []byte {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		panic("tokens: read random bytes: " + err.Error())
	}
	return buffer
}
