package passcode

import (
	"context"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

// SetPasscode stores a staff member's POS code.
//
// Passed already hashed: a passcode is a credential, and the only difference
// between it and a password is how few characters it has — which is an
// argument for hashing it more carefully, not less.
// sealed is the encrypted copy admin can reveal (nil when none).
func (s *Repo) SetPasscode(ctx context.Context, staffID string, hash, sealed *string) error {
	// Admin only ever sets codes for its own business's staff.
	cond, args := shared.BusinessCond(ctx, "business_id", []any{staffID, hash, sealed})
	tag, err := s.pool.Exec(ctx,
		`UPDATE staff_users SET passcode_hash = $2, passcode_sealed = $3, updated_at = NOW() WHERE id = $1`+cond, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// StaffWithPasscodeAtBranch returns everyone at one outlet who can sign in to
// its till.
//
// The passcode cannot be looked up directly — it is hashed, and a hash is not
// something you can index a search on — so the candidates are fetched and
// compared one by one. That is bounded by the staff of a single shop, which is
// a handful of rows, and it keeps the comparison in constant time per row
// rather than leaking which codes exist through timing.
//
// A staff member counts as "at" the outlet if they hold a role scoped to it,
// to its franchise, or to the brand — a regional manager covering a shift is
// not a different kind of person to the till.
func (s *Repo) StaffWithPasscodeAtBranch(ctx context.Context, branchID string) ([]identityapi.StaffPasscodeCandidate, error) {
	const q = `
		SELECT DISTINCT u.id, u.email, u.first_name, u.last_name, u.passcode_hash
		  FROM staff_users u
		  JOIN staff_role_assignments r ON r.staff_user_id = u.id
		 WHERE u.is_active
		   AND u.deactivated_at IS NULL
		   AND u.passcode_hash IS NOT NULL
			   AND ((r.scope_type = 'branch' AND r.branch_id = $1)
			        OR (r.scope_type = 'brand' AND ` + brandOfBranch + `))`

	// Either id can be absent — a brand-wide account has no branch, and the
	// caller may not know the franchise. An empty string is not a uuid, so it
	// goes as NULL and the comparison simply does not match.
	rows, err := s.pool.Query(ctx, q, shared.NilIfBlank(branchID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []identityapi.StaffPasscodeCandidate{}
	for rows.Next() {
		var c identityapi.StaffPasscodeCandidate
		if err := rows.Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.PasscodeHash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PasscodeTakenAtBranch reports whether another member of staff at the same
// outlet already uses this code.
//
// Checked before saving so the clash is a readable message on the form rather
// than a surprise at the till, where two people would find that one of them
// signs in as the other.
func (s *Repo) PasscodeTakenAtBranch(ctx context.Context, branchID, excludeStaffID string, matches func(hash string) bool) (bool, error) {
	candidates, err := s.StaffWithPasscodeAtBranch(ctx, branchID)
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
