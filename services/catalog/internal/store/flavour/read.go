package flavour

import (
	"context"
	"fmt"
	"strings"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

// ListFlavours lists flavours in card order; status "" means all, search
// matches the name.
func (s *Repo) ListFlavours(ctx context.Context, status, search string) ([]catalogapi.Flavour, error) {
	q, args := flavourScoped, []any{tenant.BusinessParam(ctx)}
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(" AND f.status = $%d", len(args))
	}
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+shared.LikeEscape(search)+"%")
		q += fmt.Sprintf(" AND f.name ILIKE $%d ESCAPE '\\'", len(args))
	}
	rows, err := s.pool.Query(ctx, q+flavourOrder, args...)
	if err != nil {
		return nil, err
	}
	return scanFlavours(rows)
}

// ListFlavoursInUse is the home page's cards: active flavours with at least
// one active product, in card order.
func (s *Repo) ListFlavoursInUse(ctx context.Context) ([]catalogapi.Flavour, error) {
	rows, err := s.pool.Query(ctx, storefrontSelect+flavourOrder, tenant.BusinessParam(ctx))
	if err != nil {
		return nil, err
	}
	return scanFlavours(rows)
}

// GetFlavour reads one flavour with its mapped product ids.
func (s *Repo) GetFlavour(ctx context.Context, id string) (*catalogapi.Flavour, error) {
	f, err := scanFlavour(s.pool.QueryRow(ctx, flavourScoped+" AND f.id::text = $2", tenant.BusinessParam(ctx), id))
	if err != nil {
		return nil, shared.Classify(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pf.product_id::text FROM product_flavours pf JOIN products p ON p.id = pf.product_id
		 WHERE pf.flavour_id = $1 AND p.deleted_at IS NULL ORDER BY lower(p.name)`, f.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	f.ProductIDs = []string{}
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			return nil, err
		}
		f.ProductIDs = append(f.ProductIDs, pid)
	}
	return f, rows.Err()
}
