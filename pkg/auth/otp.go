package auth

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// NewOTP generates a 6-digit numeric one-time code as used for account
// verification and password reset (ACC-01, ACC-02). Store only its hash.
func NewOTP() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
