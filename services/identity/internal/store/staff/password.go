package staff

import (
	"context"
	"time"
)

func (s *Repo) SetStaffPasswordResetToken(ctx context.Context, staffID, tokenHash string, expiresAt time.Time) error {
	const q = `UPDATE staff_users SET password_reset_hash = $2, password_reset_expires = $3 WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, staffID, tokenHash, expiresAt)
	return err
}

func (s *Repo) GetStaffPasswordResetToken(ctx context.Context, staffID string) (tokenHash string, expiresAt time.Time, err error) {
	const q = `SELECT COALESCE(password_reset_hash,''), COALESCE(password_reset_expires, NOW()) FROM staff_users WHERE id = $1`
	err = s.pool.QueryRow(ctx, q, staffID).Scan(&tokenHash, &expiresAt)
	return
}

func (s *Repo) UpdateStaffPassword(ctx context.Context, staffID, passwordHash string) error {
	const q = `UPDATE staff_users SET password_hash = $2, password_reset_hash = NULL, password_reset_expires = NULL, updated_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, staffID, passwordHash)
	return err
}
