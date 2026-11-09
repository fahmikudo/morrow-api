// Package secretbox encrypts small secrets (OAuth tokens) with AES-GCM.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Box encrypts and decrypts strings with a 32-byte key.
type Box struct {
	aead cipher.AEAD
}

// New decodes a base64 32-byte key. An empty value returns a disabled box,
// which makes OAuth unavailable rather than storing plaintext.
func New(encoded string) (*Box, error) {
	if encoded == "" {
		return &Box{}, nil
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY must be base64: %w", err)
	}
	if len(key) != 32 {
		// Accept any sufficiently long base64 value: normalize it to a 32-byte
		// AES key so a 48-byte `openssl rand -base64 48` key still works.
		if len(key) < 16 {
			return nil, errors.New("APP_ENCRYPTION_KEY must decode to at least 16 bytes")
		}
		sum := sha256.Sum256(key)
		key = sum[:]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Box{aead: aead}, nil
}

// Enabled reports whether a key was configured.
func (b *Box) Enabled() bool {
	return b != nil && b.aead != nil
}

// Seal encrypts a value and returns base64(nonce || ciphertext).
func (b *Box) Seal(value string) (string, error) {
	if !b.Enabled() {
		return "", errors.New("APP_ENCRYPTION_KEY is not configured")
	}

	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := b.aead.Seal(nil, nonce, []byte(value), nil)
	return base64.RawStdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

// Open decrypts a value produced by Seal.
func (b *Box) Open(encoded string) (string, error) {
	if !b.Enabled() {
		return "", errors.New("APP_ENCRYPTION_KEY is not configured")
	}

	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	nonceSize := b.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("secret is too short")
	}

	plain, err := b.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", err
	}

	return string(plain), nil
}
