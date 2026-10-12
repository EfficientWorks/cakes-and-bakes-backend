package tenant

import (
	"context"
	"net/http"
	"sync"

	"bakehouse/pkg/auth"
)

/*
 * The business a request acts for.
 *
 * Every query on business-owned data (products, categories, customers,
 * orders, …) filters by it, so one business can never read or change
 * another's rows. It is set once per request by middleware — from the staff
 * token for the admin, POS and KDS, from the storefront's business for
 * customers — and stores read it with BusinessParam.
 *
 * An account created before businesses existed has none: it then works on
 * the rows that have none either, which is exactly the data it always had.
 *
 * A context that never went through that middleware — a webhook, a
 * background job — is not scoped at all: it acts for the system and may reach
 * any row by id. Scoped tells the two apart.
 */

type businessKey struct{}

type scopedKey struct{}

// WithBusiness returns a context acting for businessID ("" = none).
func WithBusiness(ctx context.Context, businessID string) context.Context {
	return context.WithValue(context.WithValue(ctx, businessKey{}, businessID), scopedKey{}, true)
}

// Scoped reports whether the request acts for a business (or for the
// pre-business data). False only for system work such as webhooks.
func Scoped(ctx context.Context) bool {
	scoped, _ := ctx.Value(scopedKey{}).(bool)
	return scoped
}

// OwnsRow reports whether a row owned by rowBusinessID may be used by this
// request: always for system work, otherwise only within the same business.
func OwnsRow(ctx context.Context, rowBusinessID *string) bool {
	if !Scoped(ctx) {
		return true
	}
	mine := BusinessID(ctx)
	if rowBusinessID == nil {
		return mine == ""
	}
	return *rowBusinessID == mine
}

// BusinessID is the business the request acts for, or "".
func BusinessID(ctx context.Context) string {
	id, _ := ctx.Value(businessKey{}).(string)
	return id
}

// BusinessParam is the value for a `business_id IS NOT DISTINCT FROM $n`
// condition: the business id, or nil (SQL NULL) when there is none.
func BusinessParam(ctx context.Context) *string {
	if id := BusinessID(ctx); id != "" {
		return &id
	}
	return nil
}

// BusinessFromStaff puts the signed-in staff member's business on the
// request. Mount it after the staff auth middleware.
func BusinessFromStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims, ok := auth.StaffFromContext(r.Context()); ok && claims != nil {
			r = r.WithContext(WithBusiness(r.Context(), claims.BusinessID))
		}
		next.ServeHTTP(w, r)
	})
}

/*
 * Which business a store belongs to, for brand-wide roles.
 *
 * A brand-wide role (owner, super admin, finance) may act on every store —
 * of its own business. CanAccessBranch asks this lookup, so a request naming
 * another business's store id is refused everywhere at once. A store never
 * changes business, so an answer is cached for the life of the process.
 */

// BranchBusinessLookup returns the business id of a store ("" for a store
// from before businesses existed) and whether the store exists.
type BranchBusinessLookup func(branchID string) (businessID string, found bool, err error)

var (
	branchLookup BranchBusinessLookup
	branchOwners sync.Map // branch id → business id
)

// SetBranchBusinessLookup installs the lookup. Called once at startup; until
// then (unit tests) brand-wide roles are not checked against a business.
func SetBranchBusinessLookup(fn BranchBusinessLookup) { branchLookup = fn }

// branchInBusiness reports whether branchID belongs to businessID.
func branchInBusiness(branchID, businessID string) bool {
	if branchLookup == nil || branchID == "" {
		return true
	}
	if owner, ok := branchOwners.Load(branchID); ok {
		return owner.(string) == businessID
	}
	owner, found, err := branchLookup(branchID)
	if err != nil || !found {
		return false
	}
	branchOwners.Store(branchID, owner)
	return owner == businessID
}
