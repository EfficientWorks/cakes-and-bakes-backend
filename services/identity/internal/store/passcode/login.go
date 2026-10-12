package passcode

import (
	"context"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func (s *Repo) scanCandidates(ctx context.Context, q string, args ...any) ([]identityapi.StaffPasscodeCandidate, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identityapi.StaffPasscodeCandidate{}
	for rows.Next() {
		var c identityapi.StaffPasscodeCandidate
		if err := rows.Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.PasscodeHash, &c.Active); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// StaffWithPasscodeForPOS is StaffWithPasscodeAtBranch without the people who
// only work the Kitchen Display.
func (s *Repo) StaffWithPasscodeForPOS(ctx context.Context, branchID string) ([]identityapi.StaffPasscodeCandidate, error) {
	return s.scanCandidates(ctx, `
		SELECT DISTINCT u.id, u.email, u.first_name, u.last_name, u.passcode_hash,
		       (u.is_active AND u.deactivated_at IS NULL) AS active
		  FROM staff_users u
		  JOIN staff_role_assignments r ON r.staff_user_id = u.id
		 WHERE u.passcode_hash IS NOT NULL
		   AND r.role <> 'central_kitchen_staff'
		   AND ((r.scope_type = 'branch' AND r.branch_id = $1) OR (r.scope_type = 'brand' AND `+brandOfBranch+`))`, shared.NilIfBlank(branchID))
}

// StaffWithPasscodeForKDSBranch is who may open a store's KDS.
func (s *Repo) StaffWithPasscodeForKDSBranch(ctx context.Context, branchID string) ([]identityapi.StaffPasscodeCandidate, error) {
	return s.scanCandidates(ctx, `
		SELECT DISTINCT u.id, u.email, u.first_name, u.last_name, u.passcode_hash,
		       (u.is_active AND u.deactivated_at IS NULL) AS active
		  FROM staff_users u
		  JOIN staff_role_assignments r ON r.staff_user_id = u.id
		 WHERE u.passcode_hash IS NOT NULL
		   AND ((r.role = 'central_kitchen_staff' AND r.scope_type = 'branch' AND r.branch_id = $1)
		        OR (r.role = 'super_admin' AND r.scope_type = 'brand' AND `+brandOfBranch+`))`, shared.NilIfBlank(branchID))
}

// StaffWithPasscodeForKDSKitchen is who may open a central kitchen's KDS.
func (s *Repo) StaffWithPasscodeForKDSKitchen(ctx context.Context, centralKitchenID string) ([]identityapi.StaffPasscodeCandidate, error) {
	return s.scanCandidates(ctx, `
		SELECT DISTINCT u.id, u.email, u.first_name, u.last_name, u.passcode_hash,
		       (u.is_active AND u.deactivated_at IS NULL) AS active
		  FROM staff_users u
		  JOIN staff_role_assignments r ON r.staff_user_id = u.id
		 WHERE u.passcode_hash IS NOT NULL
		   AND ((r.role = 'central_kitchen_staff' AND r.scope_type = 'central_kitchen' AND r.central_kitchen_id = $1)
		        OR (r.role = 'super_admin' AND r.scope_type = 'brand' AND `+brandOfKitchen+`))`, shared.NilIfBlank(centralKitchenID))
}

// PasscodeTakenAtCentralKitchen reports whether somebody else at the same
// kitchen already uses the code.
func (s *Repo) PasscodeTakenAtCentralKitchen(ctx context.Context, centralKitchenID, excludeStaffID string, matches func(hash string) bool) (bool, error) {
	candidates, err := s.scanCandidates(ctx, `
		SELECT DISTINCT u.id, u.email, u.first_name, u.last_name, u.passcode_hash,
		       (u.is_active AND u.deactivated_at IS NULL) AS active
		  FROM staff_users u
		  JOIN staff_role_assignments r ON r.staff_user_id = u.id
		 WHERE u.passcode_hash IS NOT NULL
		   AND r.scope_type = 'central_kitchen' AND r.central_kitchen_id = $1`, shared.NilIfBlank(centralKitchenID))
	if err != nil {
		return false, err
	}
	for _, c := range candidates {
		if c.ID == excludeStaffID || c.PasscodeHash == nil {
			continue
		}
		if matches(*c.PasscodeHash) {
			return true, nil
		}
	}
	return false, nil
}
