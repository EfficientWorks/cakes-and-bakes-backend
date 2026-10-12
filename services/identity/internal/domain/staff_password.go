package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/identity/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

// ChangeStaffPassword lets a signed-in staff member replace their own password.
//
// The current password is required and checked, so a borrowed unlocked laptop
// cannot be used to lock the real owner out of their own account. Twelve
// characters is the floor because these accounts move money and see every
// customer's address.
//
// Only the holder can change their own: an administrator resetting someone
// else's would need its own route, its own permission and its own audit entry,
// and none of those exist yet.
func (s *Service) ChangeStaffPassword(ctx context.Context, staffID, currentPassword, newPassword string) error {
	if staffID == "" {
		return apperr.Unauthenticated("Sign in again to change your password.")
	}
	if currentPassword == "" {
		return apperr.Validation("Enter your current password.").
			WithFields(apperr.FieldError{Field: "currentPassword", Code: "REQUIRED", Message: "Required."})
	}
	if len([]rune(newPassword)) < 12 {
		return apperr.Validation("Use at least 12 characters — this account can move money.").
			WithFields(apperr.FieldError{Field: "newPassword", Code: "TOO_SHORT", Message: "At least 12 characters."})
	}
	if newPassword == currentPassword {
		return apperr.Validation("The new password is the same as the current one.").
			WithFields(apperr.FieldError{Field: "newPassword", Code: "UNCHANGED", Message: "Choose a different password."})
	}

	staff, err := s.store.GetStaffByID(ctx, staffID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.Unauthenticated("Sign in again to change your password.")
		}
		return apperr.Internal(err)
	}

	ok, err := auth.VerifySecret(staff.PasswordHash, currentPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		// Deliberately about the current password only: the caller is already
		// authenticated, so there is nothing to protect by being vague, and
		// "incorrect password" sends them to the wrong field.
		return apperr.Validation("That is not your current password.").
			WithFields(apperr.FieldError{Field: "currentPassword", Code: "INCORRECT", Message: "Incorrect."})
	}

	hash, err := auth.HashSecret(newPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	if err := s.store.UpdateStaffPassword(ctx, staffID, hash); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) UpdateOwnStaffProfile(ctx context.Context, staffID string, firstName, lastName, email, phone string) error {
	if staffID == "" {
		return apperr.Unauthenticated("Sign in again to continue.")
	}

	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)
	email = strings.TrimSpace(email)
	phone = strings.TrimSpace(phone)

	// if firstName == "" || lastName == "" {
	// 	return apperr.Validation("First name and last name are required.")
	// }

	// if email == "" || !strings.Contains(email, "@") {
	// 	return apperr.Validation("A valid email is required.")
	// }

	if _, err := s.store.GetStaffByID(ctx, staffID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.Unauthenticated("Sign in again to continue.")
		}
		return apperr.Internal(err)
	}

	if err := s.store.UpdateOwnStaffProfile(ctx, staffID, firstName, lastName, email, phone); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.Validation("That email is already in use.")
		}
		return apperr.Internal(err)
	}
	return nil
}
