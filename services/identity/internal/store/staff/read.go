package staff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func (s *Repo) GetStaffByEmail(ctx context.Context, email string) (*identityapi.StaffUser, error) {
	const q = `
		SELECT id, email, password_hash, first_name, last_name, is_active, created_at, updated_at, business_id
		FROM staff_users WHERE lower(email) = lower($1)`
	row := s.pool.QueryRow(ctx, q, email)
	return scanStaff(row)
}

func (s *Repo) GetStaffByID(ctx context.Context, id string) (*identityapi.StaffUser, error) {
	const q = `
		SELECT id, email, password_hash, first_name, last_name, is_active, created_at, updated_at, business_id
		FROM staff_users WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	return scanStaff(row)
}

// StaffByID reads one account, for the session a successful passcode opens.
func (s *Repo) StaffByID(ctx context.Context, id string) (*identityapi.StaffUser, error) {
	const q = `
		SELECT id, email, first_name, last_name, is_active, created_at, updated_at, business_id
		  FROM staff_users WHERE id = $1 AND deactivated_at IS NULL`
	var u identityapi.StaffUser
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.BusinessID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// ListStaffNetwork returns the network-wide staff directory (SA-16). A
// staff member's full role-assignment set is always returned even when the
// filters only matched one of their assignments (see identityapi.StaffDirectoryFilter).
func (s *Repo) ListStaffNetwork(ctx context.Context, f identityapi.StaffDirectoryFilter) ([]identityapi.StaffDirectoryEntry, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.IsActive != nil {
		add("su.is_active = $%d", *f.IsActive)
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		idx := len(args)
		where = append(where, fmt.Sprintf("(su.email ILIKE $%d OR su.first_name ILIKE $%d OR su.last_name ILIKE $%d)", idx, idx, idx))
	}
	if f.Role != "" || f.BranchID != "" || f.CentralKitchenID != "" {
		var sub []string
		if f.Role != "" {
			args = append(args, f.Role)
			sub = append(sub, fmt.Sprintf("sra.role = $%d", len(args)))
		}
		if f.BranchID != "" {
			args = append(args, f.BranchID)
			sub = append(sub, fmt.Sprintf("sra.branch_id = $%d", len(args)))
		}
		if f.CentralKitchenID != "" {
			args = append(args, f.CentralKitchenID)
			sub = append(sub, fmt.Sprintf("sra.central_kitchen_id = $%d", len(args)))
		}
		subClause := sub[0]
		for i := 1; i < len(sub); i++ {
			subClause += " AND " + sub[i]
		}
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM staff_role_assignments sra WHERE sra.staff_user_id = su.id AND %s)", subClause))
	}

	whereClause := ""
	for i, w := range where {
		if i > 0 {
			whereClause += " AND "
		}
		whereClause += w
	}

	q := `
		SELECT su.id, su.email, su.first_name, su.last_name, su.is_active, su.created_at, su.updated_at,
		       su.passcode_hash IS NOT NULL, su.passcode_sealed,
		       COALESCE((
		         SELECT json_agg(json_build_object(
		             'id', sra.id, 'role', sra.role, 'scopeType', sra.scope_type,
		             'branchId', sra.branch_id, 'centralKitchenId', sra.central_kitchen_id
		           ))
		         FROM staff_role_assignments sra WHERE sra.staff_user_id = su.id
		       ), '[]') AS roles_json,
		       (SELECT MAX(rt.created_at) FROM refresh_tokens rt WHERE rt.subject_type = 'staff' AND rt.subject_id = su.id) AS last_login_at
		FROM staff_users su
		WHERE ` + whereClause + `
		ORDER BY su.created_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identityapi.StaffDirectoryEntry
	for rows.Next() {
		var e identityapi.StaffDirectoryEntry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.Email, &e.FirstName, &e.LastName, &e.IsActive, &e.CreatedAt, &e.UpdatedAt, &e.HasPasscode, &e.PasscodeSealed, &raw, &e.LastLoginAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &e.Roles)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Repo) ListStaffRoleAssignments(ctx context.Context, staffUserID string) ([]identityapi.StaffRoleAssignment, error) {
	const q = `
		SELECT id, staff_user_id, role, scope_type, branch_id, central_kitchen_id,
		       max_refund_cents, max_discount_pct, order_void_window_mins, created_at
		FROM staff_role_assignments WHERE staff_user_id = $1`
	rows, err := s.pool.Query(ctx, q, staffUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identityapi.StaffRoleAssignment
	for rows.Next() {
		var ra identityapi.StaffRoleAssignment
		if err := rows.Scan(&ra.ID, &ra.StaffUserID, &ra.Role, &ra.ScopeType, &ra.BranchID, &ra.CentralKitchenID,
			&ra.MaxRefundCents, &ra.MaxDiscountPct, &ra.OrderVoidWindowMins, &ra.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ra)
	}
	return out, rows.Err()
}
