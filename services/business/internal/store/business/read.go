package business

import (
	"context"
	"strings"

	"bakehouse/services/business/businessapi"
)

func (r *Repo) GetBusiness(ctx context.Context, id string) (*businessapi.Business, error) {
	return scanBusiness(r.pool.QueryRow(ctx, selectBusiness+` WHERE id = $1`, id))
}

// ListBusinesses returns businesses newest first. search matches the name or
// slug; status filters when set.
func (r *Repo) ListBusinesses(ctx context.Context, search, status string) ([]businessapi.Business, error) {
	q := selectBusiness + ` WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR slug ILIKE '%' || $1 || '%')
		AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, strings.TrimSpace(search), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []businessapi.Business{}
	for rows.Next() {
		b, err := scanBusiness(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}
