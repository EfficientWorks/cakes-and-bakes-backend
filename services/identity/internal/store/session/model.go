package session

import "time"

type refreshTokenRow struct {
	ID        string
	SubjectID string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	// Where a POS / KDS session signed in; empty for admin sessions.
	LocationBranchID  string
	LocationKitchenID string
}
