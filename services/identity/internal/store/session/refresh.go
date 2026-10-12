package session

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/identity/internal/store/shared"
)

func (s *Repo) CreateRefreshToken(ctx context.Context, subjectType, subjectID, tokenHash string, expiresAt time.Time, ip string) (string, error) {
	return s.CreateRefreshTokenAt(ctx, subjectType, subjectID, tokenHash, expiresAt, ip, "", "")
}

// CreateRefreshTokenAt stores a refresh token for a session tied to a store
// or central kitchen (POS / KDS), so a refresh keeps the location.
func (s *Repo) CreateRefreshTokenAt(ctx context.Context, subjectType, subjectID, tokenHash string, expiresAt time.Time, ip, branchID, kitchenID string) (string, error) {
	const q = `
		INSERT INTO refresh_tokens (subject_type, subject_id, token_hash, expires_at, created_ip,
			location_branch_id, location_kitchen_id)
		VALUES ($1,$2,$3,$4,$5, NULLIF($6,'')::uuid, NULLIF($7,'')::uuid) RETURNING id`
	var id string
	err := s.pool.QueryRow(ctx, q, subjectType, subjectID, tokenHash, expiresAt, ip, branchID, kitchenID).Scan(&id)
	return id, err
}

func (s *Repo) GetRefreshTokenByHash(ctx context.Context, subjectType, tokenHash string) (*refreshTokenRow, error) {
	const q = `
		SELECT id, subject_id, token_hash, expires_at, revoked_at,
		       COALESCE(location_branch_id::text, ''), COALESCE(location_kitchen_id::text, '')
		FROM refresh_tokens WHERE subject_type = $1 AND token_hash = $2`
	var t refreshTokenRow
	err := s.pool.QueryRow(ctx, q, subjectType, tokenHash).Scan(&t.ID, &t.SubjectID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt,
		&t.LocationBranchID, &t.LocationKitchenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &t, err
}

func (s *Repo) RevokeRefreshToken(ctx context.Context, id, replacedBy string) error {
	const q = `UPDATE refresh_tokens SET revoked_at = NOW(), replaced_by = NULLIF($2,'')::uuid WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, replacedBy)
	return err
}

func (s *Repo) RevokeAllRefreshTokens(ctx context.Context, subjectType, subjectID string) error {
	const q = `UPDATE refresh_tokens SET revoked_at = NOW() WHERE subject_type = $1 AND subject_id = $2 AND revoked_at IS NULL`
	_, err := s.pool.Exec(ctx, q, subjectType, subjectID)
	return err
}
