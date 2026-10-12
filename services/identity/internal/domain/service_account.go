package domain

import (
	"context"
	"errors"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	mail "bakehouse/pkg/email"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store"
)

// --- Password reset --------------------------------------------------------

// ForgotPassword always reports success, even for an unknown email, so the
// response cannot be used to enumerate registered addresses.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = normaliseEmail(email)
	if ok, err := s.limiter.Allow(ctx, "otp-resend:"+email, otpResendLimit, otpResendWindow); err == nil && !ok {
		return nil
	}
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return nil
	}
	return s.issueAndSendOTP(ctx, customer.ID, customer.Email, mail.TemplatePasswordResetOTP)
}

// VerifyResetOTP checks the reset code and returns a short-lived reset token
// to present to ResetPasswordForEmail.
func (s *Service) VerifyResetOTP(ctx context.Context, email, otp string) (string, error) {
	email = normaliseEmail(email)
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return "", apperr.New("OTP_INVALID", apperr.CategoryValidation, "Invalid or expired code.")
	}
	hash, expiresAt, attempts, err := s.store.GetCustomerOTP(ctx, customer.ID)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if attempts >= otpMaxAttempts {
		return "", apperr.New("OTP_LOCKED", apperr.CategoryRateLimited, "Too many attempts. Request a new code.")
	}
	if hash == "" || time.Now().After(expiresAt) {
		return "", apperr.New("OTP_EXPIRED", apperr.CategoryValidation, "This code has expired. Request a new one.")
	}
	ok, err := auth.VerifySecret(hash, otp)
	if err != nil || !ok {
		_ = s.store.IncrementCustomerOTPAttempts(ctx, customer.ID)
		return "", apperr.New("OTP_INVALID", apperr.CategoryValidation, "Invalid or expired code.")
	}

	resetToken, err := auth.NewOpaqueToken()
	if err != nil {
		return "", apperr.Internal(err)
	}
	if err := s.store.SetCustomerPasswordResetToken(ctx, customer.ID, auth.HashOpaqueToken(resetToken), time.Now().Add(passwordResetTTL)); err != nil {
		return "", apperr.Internal(err)
	}
	return resetToken, nil
}

// ResetPasswordForEmail completes a reset: email + resetToken together
// identify the account, mirroring the refresh-token verification pattern.
func (s *Service) ResetPasswordForEmail(ctx context.Context, email, resetToken, newPassword, confirmPassword string) error {
	if newPassword != confirmPassword {
		return apperr.Validation("Password and confirmation do not match.").
			WithFields(apperr.FieldError{Field: "confirmPassword", Code: "MISMATCH", Message: "Does not match password."})
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	email = normaliseEmail(email)
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return apperr.New("RESET_TOKEN_INVALID", apperr.CategoryValidation, "This reset link is invalid or has expired.")
	}
	storedHash, expiresAt, err := s.store.GetCustomerPasswordResetToken(ctx, customer.ID)
	if err != nil {
		return apperr.Internal(err)
	}
	if storedHash == "" || time.Now().After(expiresAt) || storedHash != auth.HashOpaqueToken(resetToken) {
		return apperr.New("RESET_TOKEN_INVALID", apperr.CategoryValidation, "This reset link is invalid or has expired.")
	}
	hash, err := auth.HashSecret(newPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	if err := s.store.UpdateCustomerPassword(ctx, customer.ID, hash); err != nil {
		return apperr.Internal(err)
	}
	_ = s.store.RevokeAllRefreshTokens(ctx, identityapi.SubjectCustomer, customer.ID)
	return nil
}

// --- Account self-service --------------------------------------------------

func (s *Service) GetProfile(ctx context.Context, customerID string) (*identityapi.Customer, error) {
	c, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil {
		return nil, apperr.NotFound("Account not found.")
	}
	return c, nil
}

func (s *Service) UpdateProfile(ctx context.Context, customerID, firstName, lastName, mobile string) (*identityapi.Customer, error) {
	if err := s.saveCustomerProfile(ctx, customerID, firstName, lastName, mobile); err != nil {
		return nil, err
	}
	return s.store.GetCustomerByID(ctx, customerID)
}

func (s *Service) ChangePassword(ctx context.Context, customerID, currentPassword, newPassword, confirmPassword string) error {
	customer, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil {
		return apperr.NotFound("Account not found.")
	}
	ok, err := auth.VerifySecret(customer.PasswordHash, currentPassword)
	if err != nil || !ok {
		return apperr.New("WRONG_CURRENT_PASSWORD", apperr.CategoryAuthentication, "Current password is incorrect.")
	}
	if newPassword != confirmPassword {
		return apperr.Validation("Password and confirmation do not match.")
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	hash, err := auth.HashSecret(newPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.store.UpdateCustomerPassword(ctx, customerID, hash)
}

func (s *Service) DeleteAccount(ctx context.Context, customerID, password string) error {
	customer, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil {
		return apperr.NotFound("Account not found.")
	}
	ok, err := auth.VerifySecret(customer.PasswordHash, password)
	if err != nil || !ok {
		return apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect password.")
	}
	if err := s.store.SoftDeleteCustomer(ctx, customerID); err != nil {
		return apperr.Internal(err)
	}
	_ = s.store.RevokeAllRefreshTokens(ctx, identityapi.SubjectCustomer, customerID)
	return nil
}

func (s *Service) SetMarketingOptIn(ctx context.Context, customerID string, optIn bool, ip string) error {
	return s.store.SetCustomerMarketingOptIn(ctx, customerID, optIn, ip)
}

// --- Address book (ACC-06) --------------------------------------------------

func (s *Service) ListAddresses(ctx context.Context, customerID string) ([]identityapi.CustomerAddress, error) {
	return s.store.ListCustomerAddresses(ctx, customerID)
}

func (s *Service) CreateAddress(ctx context.Context, customerID string, a identityapi.CustomerAddress) (*identityapi.CustomerAddress, error) {
	a.CustomerID = customerID
	if a.Country == "" {
		a.Country = "US"
	}
	count, err := s.store.CountCustomerAddresses(ctx, customerID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if count == 0 {
		a.IsDefault = true // first address is always the default
	}
	out, err := s.store.CreateCustomerAddress(ctx, a)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Account not found.")
		}
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) UpdateAddress(ctx context.Context, customerID, addressID string, a identityapi.CustomerAddress) (*identityapi.CustomerAddress, error) {
	if err := s.store.UpdateCustomerAddress(ctx, customerID, addressID, a); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Address not found.")
		}
		return nil, apperr.Internal(err)
	}
	return s.store.GetCustomerAddress(ctx, customerID, addressID)
}

func (s *Service) DeleteAddress(ctx context.Context, customerID, addressID string) error {
	addr, err := s.store.GetCustomerAddress(ctx, customerID, addressID)
	if err != nil {
		return apperr.NotFound("Address not found.")
	}
	if addr.IsDefault {
		count, err := s.store.CountCustomerAddresses(ctx, customerID)
		if err == nil && count > 1 {
			return apperr.New("CANNOT_DELETE_DEFAULT", apperr.CategoryValidation, "Set another address as default before deleting this one.")
		}
	}
	if err := s.store.DeleteCustomerAddress(ctx, customerID, addressID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Address not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) SetDefaultAddress(ctx context.Context, customerID, addressID string) error {
	if err := s.store.SetDefaultAddress(ctx, customerID, addressID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Address not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

// --- Recipient book (ACC-07) -------------------------------------------------

func (s *Service) ListRecipients(ctx context.Context, customerID string) ([]identityapi.CustomerRecipient, error) {
	return s.store.ListCustomerRecipients(ctx, customerID)
}

func (s *Service) CreateRecipient(ctx context.Context, customerID string, r identityapi.CustomerRecipient) (*identityapi.CustomerRecipient, error) {
	r.CustomerID = customerID
	out, err := s.store.CreateCustomerRecipient(ctx, r)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Account not found.")
		}
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) UpdateRecipient(ctx context.Context, customerID, recipientID string, r identityapi.CustomerRecipient) error {
	if err := s.store.UpdateCustomerRecipient(ctx, customerID, recipientID, r); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Recipient not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) DeleteRecipient(ctx context.Context, customerID, recipientID string) error {
	if err := s.store.DeleteCustomerRecipient(ctx, customerID, recipientID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Recipient not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}
