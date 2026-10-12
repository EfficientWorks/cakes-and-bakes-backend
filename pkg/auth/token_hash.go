package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashOpaqueToken deterministically hashes a high-entropy opaque token
// (refresh tokens, password-reset tokens, cart guest tokens) so it can be
// looked up by hash without storing the raw value. Unlike passwords/OTPs,
// these tokens already carry 256 bits of crypto/rand entropy, so a fast
// deterministic hash is the correct tool — argon2id's salting would make
// lookup-by-hash impossible.
func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
