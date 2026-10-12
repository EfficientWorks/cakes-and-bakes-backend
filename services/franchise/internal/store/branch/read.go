package branch

import (
	"context"
	"fmt"
	"strings"

	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func (s *Repo) GetBranch(ctx context.Context, id string) (*franchiseapi.Branch, error) {
	return ownBranch(ctx)(scanBranch(s.pool.QueryRow(ctx, branchSelect+` WHERE b.id = $1`, id)))
}

func (s *Repo) GetBranchByCode(ctx context.Context, code string) (*franchiseapi.Branch, error) {
	return ownBranch(ctx)(scanBranch(s.pool.QueryRow(ctx, branchSelect+` WHERE b.code = $1`, code)))
}

// ownBranch hides another business's store: it is not found, not forbidden.
func ownBranch(ctx context.Context) func(*franchiseapi.Branch, error) (*franchiseapi.Branch, error) {
	return func(b *franchiseapi.Branch, err error) (*franchiseapi.Branch, error) {
		if err != nil {
			return nil, err
		}
		if !tenant.OwnsRow(ctx, b.BusinessID) {
			return nil, shared.ErrNotFound
		}
		return b, nil
	}
}

func (s *Repo) ListBranches(ctx context.Context, status string) ([]franchiseapi.Branch, error) {
	q := branchSelect
	args := []interface{}{}
	where := []string{}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("b.status = $%d", len(args)))
	}
	// Only the caller's business's stores: the console's store list and the
	// storefront's delivery and pickup routing alike.
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		where = append(where, fmt.Sprintf("b.business_id IS NOT DISTINCT FROM $%d", len(args)))
	}
	if len(where) > 0 {
		q += " WHERE " + where[0]
		for _, w := range where[1:] {
			q += " AND " + w
		}
	}
	q += " ORDER BY b.created_at DESC"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []franchiseapi.Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// ListActiveBranchesForRouting returns every active branch — used by the
// delivery-zone matcher and pickup ranking, both of which need to scan the
// whole (small) network rather than filter in SQL by geometry we do not
// index.
func (s *Repo) ListActiveBranchesForRouting(ctx context.Context) ([]franchiseapi.Branch, error) {
	return s.ListBranches(ctx, franchiseapi.StatusActive)
}

// ScopeActive reports whether the branch a role is scoped to is still
// trading.
//
// Used at sign-in. Suspending a franchise closes its shops, but the people who
// work there could still sign in and carry on — taking orders, issuing refunds
// — because nothing on the authentication path had ever been told that the
// business they belong to had been stopped.
//
// branchID may be empty: a brand-scoped assignment has none, and is always
// active — the brand itself is not something that gets suspended.
func (s *Repo) ScopeActive(ctx context.Context, branchID string) (bool, error) {
	branchID = strings.TrimSpace(branchID)

	if branchID != "" {
		var status string
		if err := s.pool.QueryRow(ctx,
			`SELECT status FROM branches WHERE id = $1`, branchID).Scan(&status); err != nil {
			return false, err
		}
		if status != "active" {
			return false, nil
		}
		return true, nil
	}

	return true, nil
}
