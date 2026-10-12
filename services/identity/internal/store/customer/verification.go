package customer

import (
	"context"
	"time"
)

func (s *Repo) SetCustomerOTP(ctx context.Context, customerID, otpHash string, expiresAt time.Time) error {
	const q = `
		UPDATE customers SET otp_hash = $2, otp_expires_at = $3, otp_attempts = 0, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`
	_, err := s.pool.Exec(ctx, q, customerID, otpHash, expiresAt)
	return err
}

func (s *Repo) GetCustomerOTP(ctx context.Context, customerID string) (otpHash string, expiresAt time.Time, attempts int, err error) {
	const q = `SELECT COALESCE(otp_hash,''), COALESCE(otp_expires_at, NOW()), otp_attempts FROM customers WHERE id = $1`
	err = s.pool.QueryRow(ctx, q, customerID).Scan(&otpHash, &expiresAt, &attempts)
	return
}

func (s *Repo) IncrementCustomerOTPAttempts(ctx context.Context, customerID string) error {
	const q = `UPDATE customers SET otp_attempts = otp_attempts + 1 WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, customerID)
	return err
}

func (s *Repo) MarkCustomerVerified(ctx context.Context, customerID string) error {
	const q = `UPDATE customers SET is_verified = true, otp_hash = NULL, otp_expires_at = NULL, otp_attempts = 0, updated_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, customerID)
	return err
}

func (s *Repo) SetCustomerPasswordResetToken(ctx context.Context, customerID, tokenHash string, expiresAt time.Time) error {
	const q = `UPDATE customers SET password_reset_hash = $2, password_reset_expires = $3 WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, customerID, tokenHash, expiresAt)
	return err
}

func (s *Repo) GetCustomerPasswordResetToken(ctx context.Context, customerID string) (tokenHash string, expiresAt time.Time, err error) {
	const q = `SELECT COALESCE(password_reset_hash,''), COALESCE(password_reset_expires, NOW()) FROM customers WHERE id = $1`
	err = s.pool.QueryRow(ctx, q, customerID).Scan(&tokenHash, &expiresAt)
	return
}

func (s *Repo) UpdateCustomerPassword(ctx context.Context, customerID, passwordHash string) error {
	const q = `UPDATE customers SET password_hash = $2, password_reset_hash = NULL, password_reset_expires = NULL, updated_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, customerID, passwordHash)
	return err
}
