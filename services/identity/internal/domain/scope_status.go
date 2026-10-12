package domain

import (
	"context"

	"bakehouse/pkg/apperr"
	"bakehouse/services/identity/identityapi"
)

// ScopeStatusReader reports whether the business a role belongs to is still
// trading.
//
// The narrowest thing identity needs from the franchise service: one
// question, one boolean.
type ScopeStatusReader interface {
	ScopeActive(ctx context.Context, branchID string) (bool, error)
}

// UseScopeStatus lets sign-in check whether a suspended business still has
// people signing in to it.
func (s *Service) UseScopeStatus(r ScopeStatusReader) { s.scopes = r }

/*
 * Can this person still sign in?
 *
 * Suspending a franchise closes its shops, but said nothing about its
 * staff — so a suspended franchise carried on having people sign in and
 * take orders, which is exactly what suspending it was meant to stop.
 *
 * One usable assignment is enough. Staff can hold several, and somebody who
 * covers two shops should not be locked out because one of them is closed.
 * A brand-scoped assignment is always usable — head office does not stop
 * working because a franchise did.
 */
func (s *Service) assertScopeActive(ctx context.Context, assignments []identityapi.StaffRoleAssignment) error {
	if s.scopes == nil || len(assignments) == 0 {
		return nil
	}

	checkable := 0
	for _, a := range assignments {
		if a.ScopeType == "brand" {
			return nil
		}
		// A corporate-office (central kitchen) assignment is usable while the
		// kitchen is active.
		if a.ScopeType == "central_kitchen" && a.CentralKitchenID != nil && s.locations != nil {
			checkable++
			active, err := s.locations.CentralKitchenActive(ctx, *a.CentralKitchenID)
			if err != nil || active {
				return nil
			}
			continue
		}
		branchID := ""
		if a.BranchID != nil {
			branchID = *a.BranchID
		}
		if branchID == "" {
			continue
		}
		checkable++

		// A lookup that fails is not a reason to lock somebody out: the
		// franchise service being unreachable is our problem, not theirs.
		active, err := s.scopes.ScopeActive(ctx, branchID)
		if err != nil || active {
			return nil
		}
	}

	if checkable == 0 {
		return nil
	}
	/*
	 * Named for what it is, because "outlet" was often wrong.
	 *
	 * A franchise-scoped account refused because its franchise is suspended
	 * was told its "outlet" was — and there is no outlet involved. Somebody
	 * who has just been given an account reads that as a broken login rather
	 * than the suspension doing exactly what it is for.
	 */
	return apperr.New("SCOPE_SUSPENDED", apperr.CategoryAuthorization,
		"The outlet this account belongs to is suspended, so it cannot sign in. "+
			"Reactivate it, or move the account to one that is trading.")
}

/*
 * Can this till trade?
 *
 * The staff-login check above asks about a person's role assignments. A till
 * asks a narrower question: it is signing in to one named outlet, and that
 * outlet is either trading or it is not.
 *
 * Worth checking at sign-in rather than leaving it to the first order.
 * Suspending an outlet already stops orders, so without this a till at a
 * suspended shop signs in happily and then refuses every order taken on it —
 * the staff find out mid-queue instead of at the login screen.
 *
 * A failed lookup lets them in, for the same reason as above: the franchise
 * service being unreachable is our problem, not the counter's.
 */
func (s *Service) assertOutletActive(ctx context.Context, branchID string) error {
	if s.scopes == nil {
		return nil
	}
	active, err := s.scopes.ScopeActive(ctx, branchID)
	if err != nil || active {
		return nil
	}
	return apperr.New("OUTLET_SUSPENDED", apperr.CategoryAuthorization,
		"This store location is suspended. Contact your administrator.")
}
