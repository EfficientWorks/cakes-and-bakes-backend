package domain

import (
	"context"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/franchise/franchiseapi"
)

/*
 * Is this outlet trading?
 *
 * Nothing on the ordering path asked. A branch could be suspended, closed, or
 * emergency-closed for the day and still take orders all the way through
 * payment — the status was set, and then only ever read by screens.
 *
 * Suspending a franchise now suspends its branches, so this one check covers
 * that too: a shop under a suspended franchise is itself suspended, and the
 * ordering path does not need to know about the hierarchy above it.
 */
func assertBranchOpen(branch *franchiseapi.Branch) error {
	if branch == nil {
		return apperr.NotFound("Outlet not found.")
	}

	switch branch.Status {
	case franchiseapi.StatusActive:
		// Trading.
	case franchiseapi.StatusSuspended:
		return apperr.New("BRANCH_SUSPENDED", apperr.CategoryBusinessRule,
			"This store location is not taking orders at the moment. Please choose another.")
	default:
		// Pending approval, closed, or anything added later: if it is not
		// explicitly active it is not selling. Defaulting the other way would
		// mean a new status silently opened the tills.
		return apperr.New("BRANCH_UNAVAILABLE", apperr.CategoryBusinessRule,
			"This store location is not taking orders. Please choose another.")
	}

	/*
	 * An emergency close is for today only.
	 *
	 * Compared in the branch's own timezone, because "today" at a shop in New
	 * Jersey is not today in UTC for several hours either side of midnight —
	 * and those hours are exactly when a late closure matters.
	 */
	if branch.EmergencyClosedOn != nil {
		loc, err := time.LoadLocation(branch.Timezone)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		closed := branch.EmergencyClosedOn.In(loc)
		if now.Year() == closed.Year() && now.YearDay() == closed.YearDay() {
			return apperr.New("BRANCH_CLOSED_TODAY", apperr.CategoryBusinessRule,
				"This location store is closed for the rest of today. Please choose another date or outlet.")
		}
	}

	return nil
}

// branchOpenByID resolves a branch and checks it in one step, for the callers
// that only have an id.
func (s *Service) branchOpenByID(ctx context.Context, branchID string) (*franchiseapi.Branch, error) {
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.NotFound("Outlet not found.")
	}
	if err := assertBranchOpen(branch); err != nil {
		return nil, err
	}
	return branch, nil
}
