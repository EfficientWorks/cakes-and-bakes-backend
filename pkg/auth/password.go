package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. Tuned for an interactive login path, not a batch job.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// HashSecret hashes a password (or any short secret such as a 6-digit OTP)
// with argon2id and returns a self-describing encoded string safe to store.
func HashSecret(secret string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("argon2id$%d$%d$%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifySecret checks a plaintext secret against a hash produced by HashSecret.
func VerifySecret(encoded, secret string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "argon2id" {
		return false, fmt.Errorf("unrecognised hash format")
	}

	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[1], "%d", &memory); err != nil {
		return false, err
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &timeCost); err != nil {
		return false, err
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &threads); err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(secret), salt, timeCost, memory, threads, uint32(len(want)))

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
