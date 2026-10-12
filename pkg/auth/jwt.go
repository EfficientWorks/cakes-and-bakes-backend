package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"bakehouse/pkg/apperr"
)

// Issuer signs and verifies access tokens for one realm (customer or staff)
// using a shared HMAC secret. Refresh tokens are opaque random strings
// stored hashed in the database, not JWTs — they carry no claims worth
// forging and are the thing we actually want to be able to revoke.
type Issuer struct {
	secret []byte
	issuer string
}

func NewIssuer(secret, issuerName string) *Issuer {
	return &Issuer{secret: []byte(secret), issuer: issuerName}
}

// IssueCustomerAccessToken signs a short-lived customer access token.
func (i *Issuer) IssueCustomerAccessToken(customerID, email string, verified bool, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := CustomerClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   customerID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		CustomerID: customerID,
		Email:      email,
		Verified:   verified,
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	return tok, exp, err
}

// ParseCustomerAccessToken verifies and decodes a customer access token.
func (i *Issuer) ParseCustomerAccessToken(raw string) (*CustomerClaims, error) {
	claims := &CustomerClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		return i.secret, nil
	}, jwt.WithIssuer(i.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return nil, apperr.Unauthenticated("Session expired or invalid. Please log in again.")
	}
	return claims, nil
}

// IssueStaffAccessToken signs a short-lived staff access token carrying every
// role assignment the user holds.
func (i *Issuer) IssueStaffAccessToken(staffID, email string, roles []RoleAssignment, ttl time.Duration) (string, time.Time, error) {
	return i.IssueStaffAccessTokenAt(staffID, email, "", roles, ttl, StaffLocation{})
}

// IssueStaffAccessTokenAt is IssueStaffAccessToken for a session tied to one
// store or central kitchen (POS / KDS).
func (i *Issuer) IssueStaffAccessTokenAt(staffID, email, businessID string, roles []RoleAssignment, ttl time.Duration, loc StaffLocation) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := StaffClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   staffID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		StaffID:           staffID,
		Email:             email,
		BusinessID:        businessID,
		RoleAssignments:   roles,
		LocationBranchID:  loc.BranchID,
		LocationKitchenID: loc.KitchenID,
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	return tok, exp, err
}

// ParseStaffAccessToken verifies and decodes a staff access token.
func (i *Issuer) ParseStaffAccessToken(raw string) (*StaffClaims, error) {
	claims := &StaffClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		return i.secret, nil
	}, jwt.WithIssuer(i.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return nil, apperr.Unauthenticated("Session expired or invalid. Please log in again.")
	}
	return claims, nil
}

// NewOpaqueToken generates a URL-safe random token for refresh tokens,
// password-reset tokens, and cart guest tokens. Store only its hash.
func NewOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
