package kitchen

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
)

// GetCentralKitchen reads one kitchen with its store assignment.
func (s *Repo) GetCentralKitchen(ctx context.Context, id string) (*franchiseapi.CentralKitchen, error) {
	q := centralKitchenSelect + ` WHERE ck.id = $1 AND ck.deleted_at IS NULL`
	args := []any{id}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		q += ` AND ` + fmt.Sprintf(kitchenScoped, len(args))
	}
	ck, err := scanCentralKitchen(s.pool.QueryRow(ctx, q, args...))
	if err != nil {
		return nil, err
	}
	if err := s.attachKitchenBranches(ctx, ck); err != nil {
		return nil, err
	}
	return ck, nil
}

// ListCentralKitchens is the console's Corporate Offices list.
func (s *Repo) ListCentralKitchens(ctx context.Context, f franchiseapi.CentralKitchenFilter) ([]franchiseapi.CentralKitchen, int, error) {
	where := []string{"ck.deleted_at IS NULL"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if tenant.Scoped(ctx) {
		add(kitchenScoped, tenant.BusinessParam(ctx))
	}
	if st := strings.ToLower(strings.TrimSpace(f.Status)); st != "" && st != "all" {
		add("ck.status = $%d", st)
	}
	if term := strings.TrimSpace(f.Search); term != "" {
		args = append(args, "%"+term+"%")
		i := len(args)
		where = append(where, fmt.Sprintf(
			"(ck.name ILIKE $%d OR ck.location_code ILIKE $%d OR ck.email ILIKE $%d OR ck.phone ILIKE $%d OR ck.manager_name ILIKE $%d)",
			i, i, i, i, i))
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM central_kitchens ck WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	q := centralKitchenSelect + ` WHERE ` + cond + ` ORDER BY ck.created_at DESC LIMIT ` +
		strconv.Itoa(limit) + ` OFFSET ` + strconv.Itoa((page-1)*limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []franchiseapi.CentralKitchen{}
	for rows.Next() {
		ck, err := scanCentralKitchen(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *ck)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		if err := s.attachKitchenBranches(ctx, &out[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (s *Repo) attachKitchenBranches(ctx context.Context, ck *franchiseapi.CentralKitchen) error {
	ids, err := s.CentralKitchenBranchIDs(ctx, ck.ID)
	if err != nil {
		return err
	}
	refs, err := s.ListBranchRefs(ctx, ids)
	if err != nil {
		return err
	}
	ck.BranchIDs = ids
	ck.Branches = refs
	return nil
}

// CentralKitchenBranchIDs lists the stores a kitchen produces for.
func (s *Repo) CentralKitchenBranchIDs(ctx context.Context, centralKitchenID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ckb.branch_id::text
		  FROM central_kitchen_branches ckb
		  JOIN branches b ON b.id = ckb.branch_id
		 WHERE ckb.central_kitchen_id = $1
		 ORDER BY b.name`, centralKitchenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListBranchRefs returns name tags for the given stores, in name order.
func (s *Repo) ListBranchRefs(ctx context.Context, ids []string) ([]franchiseapi.BranchRef, error) {
	out := []franchiseapi.BranchRef{}
	if len(ids) == 0 {
		return out, nil
	}
	// Another business's stores are left out, so MissingBranchIDs reports
	// them and a kitchen can never be assigned one.
	q := `SELECT id::text, name, code, location_code, status, timezone FROM branches WHERE id::text = ANY($1)`
	args := []any{ids}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		q += ` AND business_id IS NOT DISTINCT FROM $2`
	}
	rows, err := s.pool.Query(ctx, q+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r franchiseapi.BranchRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Code, &r.LocationCode, &r.Status, &r.Timezone); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MissingBranchIDs returns which of the given ids name no store.
func (s *Repo) MissingBranchIDs(ctx context.Context, ids []string) ([]string, error) {
	found, err := s.ListBranchRefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, r := range found {
		have[r.ID] = true
	}
	var missing []string
	for _, id := range ids {
		if strings.TrimSpace(id) != "" && !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// CentralKitchenActive reports whether a kitchen exists and is trading.
func (s *Repo) CentralKitchenActive(ctx context.Context, id string) (bool, error) {
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM central_kitchens WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == franchiseapi.CentralKitchenActive, nil
}
