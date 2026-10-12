package shared

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
)

// BusinessCond is " AND <col> IS NOT DISTINCT FROM $n" with the caller's
// business appended to args, or "" (args unchanged) for system work, which
// stays unfiltered.
func BusinessCond(ctx context.Context, col string, args []any) (string, []any) {
	if !tenant.Scoped(ctx) {
		return "", args
	}
	args = append(args, tenant.BusinessParam(ctx))
	return fmt.Sprintf(" AND %s IS NOT DISTINCT FROM $%d", col, len(args)), args
}

// CustomerCond scopes a row hanging off a customer (address, recipient) to
// customers of the caller's business, so an id from another business's
// storefront token or till finds nothing.
func CustomerCond(ctx context.Context, customerCol string, args []any) (string, []any) {
	if !tenant.Scoped(ctx) {
		return "", args
	}
	args = append(args, tenant.BusinessParam(ctx))
	return fmt.Sprintf(" AND EXISTS (SELECT 1 FROM customers bc WHERE bc.id = %s AND bc.business_id IS NOT DISTINCT FROM $%d)",
		customerCol, len(args)), args
}

// Querier is the one pgx method CustomerInBusiness needs (pool or tx).
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CustomerInBusiness is ErrNotFound when a scoped request names a customer
// of another business; for inserts, which have no WHERE to carry CustomerCond.
func CustomerInBusiness(ctx context.Context, q Querier, customerID string) error {
	if !tenant.Scoped(ctx) {
		return nil
	}
	var ok bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1 AND business_id IS NOT DISTINCT FROM $2)`,
		customerID, tenant.BusinessParam(ctx)).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
