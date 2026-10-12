package kitchen

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

// BusinessIDOwner reports which location already uses a Location/Business ID:
// "branch", "central_kitchen" or "" when it is free. The excluded ids let an
// edit keep its own code.
func (s *Repo) BusinessIDOwner(ctx context.Context, businessID, excludeBranchID, excludeKitchenID string) (string, error) {
	code := strings.TrimSpace(businessID)
	if code == "" {
		return "", nil
	}
	var owner string
	err := s.pool.QueryRow(ctx, `
		SELECT 'branch' FROM branches
		 WHERE upper(btrim(location_code)) = upper($1) AND ($2 = '' OR id::text <> $2)
		UNION ALL
		SELECT 'central_kitchen' FROM central_kitchens
		 WHERE deleted_at IS NULL AND upper(btrim(location_code)) = upper($1) AND ($3 = '' OR id::text <> $3)
		LIMIT 1`, code, excludeBranchID, excludeKitchenID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return owner, err
}

// LocationByBusinessID resolves the code typed on a POS or KDS login screen
// to a store or a central kitchen. A closed store and a deleted kitchen do
// not resolve. The match is exact, case included, as typed in admin.
func (s *Repo) LocationByBusinessID(ctx context.Context, businessID string) (*franchiseapi.Location, error) {
	code := strings.TrimSpace(businessID)
	if code == "" {
		return nil, shared.ErrNotFound
	}
	var loc franchiseapi.Location
	err := s.pool.QueryRow(ctx, `
		SELECT 'branch', id::text, COALESCE(location_code, ''), name, status, timezone
		  FROM branches
		 WHERE btrim(location_code) = $1 AND status <> 'closed'
		UNION ALL
		SELECT 'central_kitchen', id::text, location_code, name, status, timezone
		  FROM central_kitchens
		 WHERE btrim(location_code) = $1 AND deleted_at IS NULL
		LIMIT 1`, code).Scan(&loc.Type, &loc.ID, &loc.LocationCode, &loc.Name, &loc.Status, &loc.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if loc.Type == franchiseapi.LocationBranch {
		loc.BranchID = loc.ID
	} else {
		loc.CentralKitchenID = loc.ID
	}
	return &loc, nil
}
