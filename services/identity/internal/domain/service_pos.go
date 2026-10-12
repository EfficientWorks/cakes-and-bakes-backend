package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store"
)

// BranchResolver turns the code typed on the POS login screen into an outlet.
//
// Declared here as the narrowest thing identity needs from the franchise
// service rather than reaching for its store: one lookup, two ids back.
type BranchResolver interface {
	BranchByBusinessID(ctx context.Context, businessID string) (branchID, name string, err error)
	BranchTaxByID(ctx context.Context, branchID string) (taxName string, taxRatePct *float64, err error)
}

const (
	// POS and KDS passcodes are exactly four digits (KDS flow): typed one-handed
	// on a number pad, and the rate limit below is what makes that safe.
	minPasscodeLength = 4
	maxPasscodeLength = 4
)

/*
 * Signing in to a till.
 *
 * Two things are typed and neither is a password: a Business ID that says
 * which shop this is, and a passcode that says who is standing at it. The
 * order matters — the Business ID narrows the search to one outlet's staff,
 * which is what allows a passcode to be four digits without every code in the
 * company having to be distinct.
 *
 * The failure message never says which half was wrong. "That Business ID does
 * not exist" tells somebody standing outside the shop whether they are one
 * guess away or ten thousand.
 */
func (s *Service) POSLogin(ctx context.Context, businessID, passcode, ip string) (*auth.TokenPair, *identityapi.StaffUser, []identityapi.StaffRoleAssignment, *identityapi.POSOutlet, error) {
	const invalid = "Incorrect Passcode."

	if ok, err := s.limiter.Allow(ctx, "pos-login:"+ip, loginRateLimit, loginRateWindow); err == nil && !ok {
		return nil, nil, nil, nil, apperr.RateLimited("Too many attempts. Please try again shortly.")
	}
	if s.branches == nil {
		return nil, nil, nil, nil, apperr.Internal(errNoBranchResolver)
	}

	businessID = strings.TrimSpace(businessID)
	passcode = strings.TrimSpace(passcode)
	if businessID == "" || passcode == "" {
		return nil, nil, nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, invalid)
	}

	// A central kitchen's Location ID opens the Kitchen Display, never a till.
	if s.locations != nil {
		if loc, locErr := s.locations.LocationByBusinessID(ctx, businessID); locErr == nil &&
			loc.Type == franchiseapi.LocationCentralKitchen {
			return nil, nil, nil, nil, errLocationNotPOS()
		}
	}

	branchID, outletName, err := s.branches.BranchByBusinessID(ctx, businessID)
	if err != nil {
		return nil, nil, nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, invalid)
	}
	if err := s.assertOutletActive(ctx, branchID); err != nil {
		return nil, nil, nil, nil, err
	}

	// Everyone at the outlet except central-kitchen staff, who work the KDS.
	candidates, err := s.store.StaffWithPasscodeForPOS(ctx, branchID)
	if err != nil {
		return nil, nil, nil, nil, apperr.Internal(err)
	}

	/*
	 * Every candidate is checked, even after one matches.
	 *
	 * Returning on the first hit would make a wrong passcode measurably faster
	 * than a right one for staff early in the list. The work here is a handful
	 * of hashes over one shop's people, so the constant cost is worth the
	 * silence.
	 */
	matched, err := matchPasscode(candidates, passcode)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	staff, err := s.store.StaffByID(ctx, matched.ID)
	if err != nil {
		return nil, nil, nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, invalid)
	}
	if !staff.IsActive {
		return nil, nil, nil, nil, errAccountDeactivated()
	}

	assignments, err := s.store.ListStaffRoleAssignments(ctx, staff.ID)
	if err != nil {
		return nil, nil, nil, nil, apperr.Internal(err)
	}
	if len(assignments) == 0 {
		return nil, nil, nil, nil, apperr.New("NO_ROLE_ASSIGNED", apperr.CategoryAuthorization, "This account has no role assigned. Contact your administrator.")
	}

	pair, err := s.issueStaffTokenPairAt(ctx, staff, assignments, auth.StaffLocation{BranchID: branchID})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// The till needs to know which shop it just opened, for every order it is
	// about to write.
	outlet := &identityapi.POSOutlet{
		BranchID:   branchID,
		BusinessID: businessID,
		Name:       outletName,
		Type:       franchiseapi.LocationBranch,
	}
	if taxName, taxRatePct, taxErr := s.branches.BranchTaxByID(ctx, branchID); taxErr == nil {
		outlet.TaxName = taxName
		outlet.TaxRatePct = taxRatePct
	}
	return pair, staff, assignments, outlet, nil
}

/*
 * Step one: is this a real till?
 *
 * The login screen asks for the Business ID first and the passcode second,
 * so the first screen needs an answer before the second exists. Doing it in
 * one call meant the operator could only be told "one of these two things
 * is wrong" after typing both — and the screen has nowhere to put that.
 *
 * This does confirm whether a Business ID exists, which the combined call
 * deliberately would not. That is the cost of the two-screen flow and it is
 * the right trade here: the code identifies a shop, not a person, it is
 * printed on the till, and knowing one buys nothing without a passcode. The
 * rate limit is what stops it being enumerated at leisure.
 *
 * It grants nothing. No token, no session — only the name of the shop, so
 * the next screen can say which one it is about to sign in to.
 */
func (s *Service) ValidateBusinessID(ctx context.Context, businessID, ip string) (*identityapi.POSOutlet, error) {
	if ok, err := s.limiter.Allow(ctx, "pos-business-id:"+ip, loginRateLimit, loginRateWindow); err == nil && !ok {
		return nil, apperr.RateLimited("Too many attempts. Please try again shortly.")
	}
	if s.branches == nil {
		return nil, apperr.Internal(errNoBranchResolver)
	}

	businessID = strings.TrimSpace(businessID)
	if businessID == "" {
		return nil, apperr.New("INVALID_BUSINESS_ID", apperr.CategoryAuthentication, "Incorrect Store ID.")
	}

	/*
	 * A Location ID now names a store or a central kitchen (KDS flow), and the
	 * answer says which in "type". The till still signs in to stores only —
	 * POSLogin refuses a kitchen — but the check itself is shared with the KDS.
	 */
	if s.locations != nil {
		if loc, locErr := s.locations.LocationByBusinessID(ctx, businessID); locErr == nil &&
			loc.Type == franchiseapi.LocationCentralKitchen {
			if loc.Status != franchiseapi.CentralKitchenActive {
				return nil, errLocationInactive()
			}
			return &identityapi.POSOutlet{
				BusinessID:       loc.BusinessID,
				Name:             loc.Name,
				Type:             franchiseapi.LocationCentralKitchen,
				CentralKitchenID: loc.CentralKitchenID,
			}, nil
		}
	}

	branchID, name, err := s.branches.BranchByBusinessID(ctx, businessID)
	if err != nil {
		return nil, apperr.New("INVALID_BUSINESS_ID", apperr.CategoryAuthentication, "Incorrect Store ID.")
	}
	if err := s.assertOutletActive(ctx, branchID); err != nil {
		return nil, err
	}

	return &identityapi.POSOutlet{
		BranchID:   branchID,
		BusinessID: businessID,
		Name:       name,
		Type:       franchiseapi.LocationBranch,
	}, nil
}

/*
 * Giving somebody a passcode.
 *
 * Only where the caller could manage that person anyway — this is a second
 * credential on an existing account, not a way around who may create staff.
 * The code is checked for uniqueness within the outlet before it is saved, so
 * a clash is a message on the form rather than two people discovering at the
 * till that one of them signs in as the other.
 */
func (s *Service) SetStaffPasscode(ctx context.Context, claims *auth.StaffClaims, staffID, passcode string) error {
	if !tenant.HasPermission(claims, tenant.PermStaffManage) {
		return apperr.PermissionDenied("You do not have permission to manage staff.")
	}

	passcode = strings.TrimSpace(passcode)

	// An empty passcode takes POS access away, which is a legitimate thing to
	// want and should not need a separate endpoint.
	if passcode == "" {
		if err := s.store.SetPasscode(ctx, staffID, nil, nil); err != nil {
			return staffNotFound(err)
		}
		return nil
	}

	if err := validatePasscodeFormat(passcode); err != nil {
		return err
	}

	assignments, err := s.store.ListStaffRoleAssignments(ctx, staffID)
	if err != nil {
		return staffNotFound(err)
	}
	if err := s.assertPasscodeFree(ctx, assignments, staffID, passcode); err != nil {
		return err
	}

	if err := s.storePasscode(ctx, staffID, passcode); err != nil {
		return staffNotFound(err)
	}
	return nil
}

// validatePasscodeFormat: exactly four digits.
func validatePasscodeFormat(passcode string) error {
	if len(passcode) < minPasscodeLength || len(passcode) > maxPasscodeLength {
		return apperr.Validation("A passcode is exactly 4 digits.").
			WithFields(apperr.FieldError{Field: "passcode", Code: "LENGTH", Message: "Exactly 4 digits."})
	}
	for _, r := range passcode {
		if r < '0' || r > '9' {
			return apperr.Validation("A passcode is digits only — it is typed on a number pad.").
				WithFields(apperr.FieldError{Field: "passcode", Code: "DIGITS_ONLY", Message: "Digits only."})
		}
	}
	return nil
}

/*
 * assertPasscodeFree checks the code is not already somebody else's at any
 * store or central kitchen this person is assigned to. Two people with the
 * same code at one location would find that one of them signs in as the
 * other.
 */
func (s *Service) assertPasscodeFree(ctx context.Context, assignments []identityapi.StaffRoleAssignment, staffID, passcode string) error {
	matches := func(hash string) bool {
		ok, verifyErr := auth.VerifySecret(hash, passcode)
		return verifyErr == nil && ok
	}
	for _, a := range assignments {
		if a.CentralKitchenID != nil && *a.CentralKitchenID != "" {
			taken, err := s.store.PasscodeTakenAtCentralKitchen(ctx, *a.CentralKitchenID, staffID, matches)
			if err != nil {
				return apperr.Internal(err)
			}
			if taken {
				return errPasscodeTaken()
			}
		}
		if a.BranchID == nil || *a.BranchID == "" {
			continue
		}
		taken, err := s.store.PasscodeTakenAtBranch(ctx, *a.BranchID, staffID, matches)
		if err != nil {
			return apperr.Internal(err)
		}
		if taken {
			return errPasscodeTaken()
		}
	}
	return nil
}

func errPasscodeTaken() error {
	return apperr.New("PASSCODE_TAKEN", apperr.CategoryConflict,
		"Somebody at this location already uses that passcode. Choose another.").
		WithFields(apperr.FieldError{Field: "passcode", Code: "TAKEN", Message: "Already used here."})
}

// errNoBranchResolver is returned when POS login is reached before the
// franchise lookup has been wired in — a start-up mistake, not a user one.
var errNoBranchResolver = errors.New("pos login: no branch resolver configured")

// staffNotFound turns a missing row into a 404 and anything else into a 500.
func staffNotFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound("Staff member not found.")
	}
	return apperr.Internal(err)
}
