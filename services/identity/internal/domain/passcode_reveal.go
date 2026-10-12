package domain

import (
	"context"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/sealbox"
)

/*
 * Revealing a till passcode in admin.
 *
 * Sign-in still checks passcode_hash. Next to it sits an encrypted copy,
 * which only staff managers see, so a manager can tell somebody their code
 * without resetting it. A code set before this existed has no copy and
 * shows as unavailable until it is set again.
 */

// UsePasscodeBox sets the key passcodes are sealed with. Nil turns the
// reveal off; sign-in is unaffected.
func (s *Service) UsePasscodeBox(b *sealbox.Box) { s.passcodes = b }

// passcodeSecrets turns a code into what is stored: its hash, and its sealed
// copy when a key is configured. An empty code clears both.
func (s *Service) passcodeSecrets(code string) (hash, sealed *string, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil, nil
	}
	h, err := auth.HashSecret(code)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	hash = &h
	if s.passcodes != nil {
		if v, err := s.passcodes.Seal(code); err == nil {
			sealed = &v
		}
	}
	return hash, sealed, nil
}

// storePasscode hashes, seals and saves one person's code.
func (s *Service) storePasscode(ctx context.Context, staffID, code string) error {
	hash, sealed, err := s.passcodeSecrets(code)
	if err != nil {
		return err
	}
	return s.store.SetPasscode(ctx, staffID, hash, sealed)
}

// RevealPasscode opens a sealed passcode, or returns "" when there is none
// or it cannot be opened.
func (s *Service) RevealPasscode(sealed *string) string {
	if sealed == nil || *sealed == "" || s.passcodes == nil {
		return ""
	}
	code, err := s.passcodes.Open(*sealed)
	if err != nil {
		return ""
	}
	return code
}
