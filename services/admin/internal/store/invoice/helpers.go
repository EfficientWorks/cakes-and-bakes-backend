package invoice

import (
	"context"
	"fmt"

	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
)

func orEmptyModifiers(m []adminapi.InvoiceItemModifier) []adminapi.InvoiceItemModifier {
	if m == nil {
		return []adminapi.InvoiceItemModifier{}
	}
	return m
}

// inBusiness is the condition that col belongs to the caller's business. $n is
// tenant.Scoped and $n+1 tenant.BusinessParam, so system work is never
// filtered. Callers pass both through businessArgs.
func inBusiness(col string, n int) string {
	return fmt.Sprintf("(NOT $%d::boolean OR %s IS NOT DISTINCT FROM $%d::uuid)", n, col, n+1)
}

func businessArgs(ctx context.Context) []any {
	return []any{tenant.Scoped(ctx), tenant.BusinessParam(ctx)}
}
