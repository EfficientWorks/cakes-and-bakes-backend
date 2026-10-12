package customer

import (
	"context"
	"time"

	"bakehouse/services/identity/internal/store/shared"
)

// Every statement here is by id, and the id always comes from a lookup that
// is already business-scoped; the condition is kept anyway so no path can
// touch another business's customer by passing its id.

func (s *Repo) SetCustomerOTP(ctx context.Context, customerID, otpHash string, expiresAt time.Time) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID, otpHash, expiresAt})
	q := `
		UPDATE customers SET otp_hash = $2, otp_expires_at = $3, otp_attempts = 0, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

func (s *Repo) GetCustomerOTP(ctx context.Context, customerID string) (otpHash string, expiresAt time.Time, attempts int, err error) {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `SELECT COALESCE(otp_hash,''), COALESCE(otp_expires_at, NOW()), otp_attempts FROM customers WHERE id = $1` + cond
	err = s.pool.QueryRow(ctx, q, args...).Scan(&otpHash, &expiresAt, &attempts)
	return
}

func (s *Repo) IncrementCustomerOTPAttempts(ctx context.Context, customerID string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `UPDATE customers SET otp_attempts = otp_attempts + 1 WHERE id = $1` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

func (s *Repo) MarkCustomerVerified(ctx context.Context, customerID string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `UPDATE customers SET is_verified = true, otp_hash = NULL, otp_expires_at = NULL, otp_attempts = 0, updated_at = NOW() WHERE id = $1` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

func (s *Repo) SetCustomerPasswordResetToken(ctx context.Context, customerID, tokenHash string, expiresAt time.Time) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID, tokenHash, expiresAt})
	q := `UPDATE customers SET password_reset_hash = $2, password_reset_expires = $3 WHERE id = $1` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

func (s *Repo) GetCustomerPasswordResetToken(ctx context.Context, customerID string) (tokenHash string, expiresAt time.Time, err error) {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID})
	q := `SELECT COALESCE(password_reset_hash,''), COALESCE(password_reset_expires, NOW()) FROM customers WHERE id = $1` + cond
	err = s.pool.QueryRow(ctx, q, args...).Scan(&tokenHash, &expiresAt)
	return
}

func (s *Repo) UpdateCustomerPassword(ctx context.Context, customerID, passwordHash string) error {
	cond, args := shared.BusinessCond(ctx, "business_id", []any{customerID, passwordHash})
	q := `UPDATE customers SET password_hash = $2, password_reset_hash = NULL, password_reset_expires = NULL, updated_at = NOW() WHERE id = $1` + cond
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}
