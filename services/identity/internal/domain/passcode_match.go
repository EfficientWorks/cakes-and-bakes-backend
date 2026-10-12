package domain

import (
	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/identity/identityapi"
)

// matchPasscode checks the passcode against every candidate (all of them, so
// a wrong passcode takes as long as a right one) and returns the active
// account it belongs to.
//
// Deactivated accounts are candidates too, so a deactivated person is told
// their account is deactivated rather than that their passcode is wrong.
// An active match always wins: passcode uniqueness ignores deactivated
// accounts, so a new colleague may hold the same four digits.
func matchPasscode(candidates []identityapi.StaffPasscodeCandidate, passcode string) (*identityapi.StaffPasscodeCandidate, error) {
	var active *identityapi.StaffPasscodeCandidate
	deactivatedMatch := false
	for i := range candidates {
		c := candidates[i]
		if c.PasscodeHash == nil {
			continue
		}
		ok, err := auth.VerifySecret(*c.PasscodeHash, passcode)
		if err != nil || !ok {
			continue
		}
		if c.Active {
			if active == nil {
				active = &candidates[i]
			}
		} else {
			deactivatedMatch = true
		}
	}
	if active != nil {
		return active, nil
	}
	if deactivatedMatch {
		return nil, errAccountDeactivated()
	}
	return nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect Passcode.")
}

func errAccountDeactivated() error {
	return apperr.New("ACCOUNT_DEACTIVATED", apperr.CategoryAuthorization,
		"This staff account has been deactivated. Contact your administrator.")
}
