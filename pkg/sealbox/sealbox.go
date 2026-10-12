// Package sealbox encrypts small values that must be read back later, such
// as a staff member's till passcode that admin can reveal. AES-256-GCM, with
// the key derived from a server secret; the output is base64 text.
package sealbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// Box seals and opens values with one key.
type Box struct {
	aead cipher.AEAD
}

// New derives the key from secret. An empty secret gives a nil Box, which
// seals nothing and opens nothing.
func New(secret string) *Box {
	if secret == "" {
		return nil
	}
	key := sha256.Sum256([]byte("bakehouse/sealbox:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil
	}
	return &Box{aead: aead}
}

// Seal encrypts plain. The nonce rides in front of the ciphertext.
func (b *Box) Seal(plain string) (string, error) {
	if b == nil {
		return "", errors.New("sealbox: no key configured")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value from Seal. A value sealed under another key fails.
func (b *Box) Open(sealed string) (string, error) {
	if b == nil {
		return "", errors.New("sealbox: no key configured")
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("sealbox: value too short")
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
