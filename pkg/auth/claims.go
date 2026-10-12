// Package auth issues and verifies the two JWT realms used across the API —
// customer (brand-level, one login for every branch) and staff (scoped per
// role/franchise/branch) — plus argon2id password/OTP hashing.
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CustomerClaims is embedded in every customer-issued access token.
type CustomerClaims struct {
	jwt.RegisteredClaims
	CustomerID string `json:"cid"`
	Email      string `json:"email"`
	Verified   bool   `json:"verified"`
}

// StaffClaims is embedded in every staff-issued access token. RoleAssignments
// carries every role assignment the user holds at brand or branch scope.
type StaffClaims struct {
	jwt.RegisteredClaims
	StaffID         string           `json:"sid"`
	Email           string           `json:"email"`
	RoleAssignments []RoleAssignment `json:"roles"`
	// The store or central kitchen a POS / KDS session signed in to. Empty
	// for admin console sessions. Every POS/KDS request re-checks that this
	// location is still trading, so closing a store logs its tills out.
	LocationBranchID  string `json:"loc,omitempty"`
	LocationKitchenID string `json:"ck,omitempty"`
}

// StaffLocation is where a POS / KDS session signed in.
type StaffLocation struct {
	BranchID  string
	KitchenID string
}

// RoleAssignment is one scoped grant: a role at the brand or at one branch.
type RoleAssignment struct {
	Role      string  `json:"role"`
	ScopeType string  `json:"scopeType"` // brand | branch | central_kitchen
	BranchID  *string `json:"branchId,omitempty"`
	// Set only on a central_kitchen scope. Omitted otherwise, so tokens for
	// every existing role are byte-for-byte what they were.
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
}

// TokenPair is what every login/refresh endpoint returns.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}
