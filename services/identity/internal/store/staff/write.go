package staff

import (
	"context"
	"fmt"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func (s *Repo) CreateStaffUser(ctx context.Context, email, passwordHash, firstName, lastName string) (*identityapi.StaffUser, error) {
	const q = `
		INSERT INTO staff_users (email, password_hash, first_name, last_name)
		VALUES ($1,$2,$3,$4)
		RETURNING id, email, password_hash, first_name, last_name, is_active, created_at, updated_at, business_id`
	row := s.pool.QueryRow(ctx, q, email, passwordHash, firstName, lastName)
	return scanStaff(row)
}

// CreateStaffUserWithRole writes the account and its role assignment together.
//
// These used to be two separate statements. The scope columns carry a CHECK
// constraint — brand scope takes no branch, branch scope takes a branch — so a mismatched pair failed on the second
// write, after the account had already been committed. What was left behind
// was a staff member with a password and no role: able to sign in, refused by
// every screen, and indistinguishable from a permissions bug.
//
// One transaction means the account and the permission that makes it usable
// arrive together or not at all.
func (s *Repo) CreateStaffUserWithRole(
	ctx context.Context,
	businessID, email, passwordHash, firstName, lastName string,
	role identityapi.StaffRoleAssignment,
) (*identityapi.StaffUser, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const createUser = `
		INSERT INTO staff_users (business_id, email, password_hash, first_name, last_name)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, email, password_hash, first_name, last_name, is_active, created_at, updated_at, business_id`
	staff, err := scanStaff(tx.QueryRow(ctx, createUser, shared.NilIfBlank(businessID), email, passwordHash, firstName, lastName))
	if err != nil {
		return nil, fmt.Errorf("create staff user: %w", err)
	}

	const createRole = `
		INSERT INTO staff_role_assignments
			(staff_user_id, role, scope_type, branch_id, central_kitchen_id,
			 max_refund_cents, max_discount_pct, order_void_window_mins)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	if _, err := tx.Exec(ctx, createRole,
		staff.ID, role.Role, role.ScopeType, role.BranchID, role.CentralKitchenID,
		role.MaxRefundCents, role.MaxDiscountPct, role.OrderVoidWindowMins); err != nil {
		return nil, fmt.Errorf("assign role %q at %q scope: %w", role.Role, role.ScopeType, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return staff, nil
}

func (s *Repo) CreateStaffRoleAssignment(ctx context.Context, ra identityapi.StaffRoleAssignment) (*identityapi.StaffRoleAssignment, error) {
	const q = `
		INSERT INTO staff_role_assignments (staff_user_id, role, scope_type, branch_id, central_kitchen_id, max_refund_cents, max_discount_pct, order_void_window_mins)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, staff_user_id, role, scope_type, branch_id, central_kitchen_id, max_refund_cents, max_discount_pct, order_void_window_mins, created_at`
	row := s.pool.QueryRow(ctx, q, ra.StaffUserID, ra.Role, ra.ScopeType, ra.BranchID, ra.CentralKitchenID, ra.MaxRefundCents, ra.MaxDiscountPct, ra.OrderVoidWindowMins)
	var out identityapi.StaffRoleAssignment
	err := row.Scan(&out.ID, &out.StaffUserID, &out.Role, &out.ScopeType, &out.BranchID, &out.CentralKitchenID, &out.MaxRefundCents, &out.MaxDiscountPct, &out.OrderVoidWindowMins, &out.CreatedAt)
	return &out, err
}

// ReplaceStaffRoleAssignment gives a staff member exactly one scoped role.
//
// Replace rather than add: the console's form offers one role at one scope, so
// saving it twice must not accumulate grants. RBAC-02 allows several
// assignments per user, and when the console grows a multi-role editor this is
// where that changes — deliberately in one place.
func (s *Repo) ReplaceStaffRoleAssignment(ctx context.Context, staffID, role, scopeType string, branchID, centralKitchenID *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM staff_role_assignments WHERE staff_user_id = $1`, staffID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO staff_role_assignments (staff_user_id, role, scope_type, branch_id, central_kitchen_id)
		VALUES ($1,$2,$3,$4,$5)`, staffID, role, scopeType, branchID, centralKitchenID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Repo) DeactivateStaffUser(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE staff_users SET is_active = false, deactivated_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ReactivateStaffUser is the counterpart to DeactivateStaffUser.
//
// It clears deactivated_at rather than only flipping the flag, so "is this
// account currently disabled" and "when was it last disabled" cannot disagree.
func (s *Repo) ReactivateStaffUser(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE staff_users SET is_active = true, deactivated_at = NULL, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// UpdateStaffProfile changes the person, not their access. Role and scope move
// through role assignments, which are a separate concern with their own audit.
func (s *Repo) UpdateStaffProfile(ctx context.Context, id, firstName, lastName string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE staff_users SET first_name = $2, last_name = $3, updated_at = NOW() WHERE id = $1`,
		id, firstName, lastName)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) UpdateOwnStaffProfile(ctx context.Context, staffID string, firstName, lastName, email, phone string) error {
	const q = `UPDATE staff_users SET first_name = $2, last_name = $3, email = $4, phone = $5, updated_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, staffID, firstName, lastName, email, phone)
	return err
}
