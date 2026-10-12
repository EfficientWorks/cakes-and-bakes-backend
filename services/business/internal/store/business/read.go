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

// GetBusinessByWebsite finds the business whose shop is served from origin
// (scheme and host, e.g. https://cakesandbakes.co.uk).
func (r *Repo) GetBusinessByWebsite(ctx context.Context, origin string) (*businessapi.Business, error) {
	return scanBusiness(r.pool.QueryRow(ctx, selectBusiness+` WHERE lower(website_url) = lower($1)`, strings.TrimSpace(origin)))
}

// GetBusinessBySlug finds a business by its slug, case-insensitively.
func (r *Repo) GetBusinessBySlug(ctx context.Context, slug string) (*businessapi.Business, error) {
	return scanBusiness(r.pool.QueryRow(ctx, selectBusiness+` WHERE lower(slug) = lower($1)`, strings.TrimSpace(slug)))
}
