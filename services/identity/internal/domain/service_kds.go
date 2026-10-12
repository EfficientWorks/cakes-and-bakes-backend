package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/identity/identityapi"
)

/*
 * Signing in to the Kitchen Display.
 *
 * The same two screens as the till — a Location ID, then a four-digit
 * passcode — but a Location ID here can name a store or a central kitchen,
 * and the people who may sign in are different:
 *
 *   store            central-kitchen staff assigned to that store (sees
 *                    that store's orders only);
 *   central kitchen  central-kitchen staff assigned to that kitchen (sees
 *                    every store the kitchen serves).
 *
 * A super admin may open either, for setting a device up. Counter and kitchen
 * staff of a store use the POS, not this.
 */

// LocationResolver is what identity needs from the franchise service to tell
// a store's Location ID from a central kitchen's.
type LocationResolver interface {
	LocationByBusinessID(ctx context.Context, businessID string) (*franchiseapi.Location, error)
	CentralKitchenActive(ctx context.Context, id string) (bool, error)
	CentralKitchenBranchIDs(ctx context.Context, centralKitchenID string) ([]string, error)
	ListBranchRefs(ctx context.Context, ids []string) ([]franchiseapi.BranchRef, error)
}

// UseLocationResolver wires the Location ID lookup (after franchise is built).
func (s *Service) UseLocationResolver(r LocationResolver) { s.locations = r }

func errLocationNotPOS() error {
	return apperr.New("LOCATION_NOT_FOR_POS", apperr.CategoryAuthorization,
		"This Location ID belongs to a corporate office. Sign in on the Kitchen Display instead.")
}

func errLocationInactive() error {
	return apperr.New("LOCATION_INACTIVE", apperr.CategoryAuthorization,
		"This location is inactive. Contact your administrator.")
}

func errInvalidLocationID() error {
	return apperr.New("INVALID_BUSINESS_ID", apperr.CategoryAuthentication, "Incorrect Location ID.")
}

// resolveKDSLocation turns a Location ID into the location and the stores its
// KDS covers, refusing a location that is not trading.
func (s *Service) resolveKDSLocation(ctx context.Context, businessID string) (*identityapi.KDSLocation, error) {
	if s.locations == nil {
		return nil, apperr.Internal(errors.New("kds login: no location resolver configured"))
	}
	businessID = strings.TrimSpace(businessID)
	if businessID == "" {
		return nil, errInvalidLocationID()
	}
	loc, err := s.locations.LocationByBusinessID(ctx, businessID)
	if err != nil {
		return nil, errInvalidLocationID()
	}

	out := &identityapi.KDSLocation{
		Type: loc.Type, BusinessID: loc.BusinessID, Name: loc.Name, Timezone: loc.Timezone,
		Branches: []identityapi.KDSBranchRef{},
	}
	var branchIDs []string
	switch loc.Type {
	case franchiseapi.LocationCentralKitchen:
		active, err := s.locations.CentralKitchenActive(ctx, loc.CentralKitchenID)
		if err == nil && !active {
			return nil, errLocationInactive()
		}
		out.CentralKitchenID = loc.CentralKitchenID
		if ids, err := s.locations.CentralKitchenBranchIDs(ctx, loc.CentralKitchenID); err == nil {
			branchIDs = ids
		}
	default:
		if err := s.assertOutletActive(ctx, loc.BranchID); err != nil {
			return nil, err
		}
		out.BranchID = loc.BranchID
		branchIDs = []string{loc.BranchID}
	}
	if refs, err := s.locations.ListBranchRefs(ctx, branchIDs); err == nil {
		for _, r := range refs {
			out.Branches = append(out.Branches, identityapi.KDSBranchRef{ID: r.ID, Name: r.Name, BusinessID: r.BusinessID})
		}
	}
	return out, nil
}

// ValidateKDSBusinessID is step one of the KDS sign-in: it confirms the
// Location ID and says whether it is a store or a central kitchen. Grants
// nothing.
func (s *Service) ValidateKDSBusinessID(ctx context.Context, businessID, ip string) (*identityapi.KDSLocation, error) {
	if ok, err := s.limiter.Allow(ctx, "kds-business-id:"+ip, loginRateLimit, loginRateWindow); err == nil && !ok {
		return nil, apperr.RateLimited("Too many attempts. Please try again shortly.")
	}
	return s.resolveKDSLocation(ctx, businessID)
}

// KDSLogin signs a KDS in with a Location ID and a four-digit passcode.
func (s *Service) KDSLogin(ctx context.Context, businessID, passcode, ip string) (*auth.TokenPair, *identityapi.StaffUser, []identityapi.StaffRoleAssignment, *identityapi.KDSLocation, error) {
	const invalid = "Incorrect Passcode."

	if ok, err := s.limiter.Allow(ctx, "kds-login:"+ip, loginRateLimit, loginRateWindow); err == nil && !ok {
		return nil, nil, nil, nil, apperr.RateLimited("Too many attempts. Please try again shortly.")
	}
	passcode = strings.TrimSpace(passcode)
	if passcode == "" {
		return nil, nil, nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, invalid)
	}

	loc, err := s.resolveKDSLocation(ctx, businessID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	var candidates []identityapi.StaffPasscodeCandidate
	if loc.Type == franchiseapi.LocationCentralKitchen {
		candidates, err = s.store.StaffWithPasscodeForKDSKitchen(ctx, loc.CentralKitchenID)
	} else {
		candidates, err = s.store.StaffWithPasscodeForKDSBranch(ctx, loc.BranchID)
	}
	if err != nil {
		return nil, nil, nil, nil, apperr.Internal(err)
	}

	// Every candidate is checked even after a match, as on the POS, so a right
	// code is not measurably faster than a wrong one.
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

	pair, err := s.issueStaffTokenPairAt(ctx, staff, assignments, auth.StaffLocation{BranchID: loc.BranchID, KitchenID: loc.CentralKitchenID})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return pair, staff, assignments, loc, nil
}
