package domain

import (
	"errors"
	"testing"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/identity/identityapi"
)

func candidate(t *testing.T, id, passcode string, active bool) identityapi.StaffPasscodeCandidate {
	t.Helper()
	h, err := auth.HashSecret(passcode)
	if err != nil {
		t.Fatal(err)
	}
	return identityapi.StaffPasscodeCandidate{ID: id, PasscodeHash: &h, Active: active}
}

func code(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestMatchPasscode(t *testing.T) {
	active := candidate(t, "active", "1234", true)
	gone := candidate(t, "gone", "4321", false)
	shared := candidate(t, "gone-shared", "1234", false)

	if c, err := matchPasscode([]identityapi.StaffPasscodeCandidate{active, gone}, "1234"); err != nil || c.ID != "active" {
		t.Fatalf("active match: %v %v", c, err)
	}
	// A deactivated person with the right passcode is told so.
	if _, err := matchPasscode([]identityapi.StaffPasscodeCandidate{active, gone}, "4321"); code(err) != "ACCOUNT_DEACTIVATED" {
		t.Fatalf("deactivated: %v", err)
	}
	// A wrong passcode is still just wrong.
	if _, err := matchPasscode([]identityapi.StaffPasscodeCandidate{active, gone}, "9999"); code(err) != "INVALID_CREDENTIALS" {
		t.Fatalf("wrong: %v", err)
	}
	// An active colleague holding the same digits as a deactivated one wins.
	if c, err := matchPasscode([]identityapi.StaffPasscodeCandidate{shared, active}, "1234"); err != nil || c.ID != "active" {
		t.Fatalf("shared digits: %v %v", c, err)
	}
}
