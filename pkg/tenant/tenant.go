package tenant

import (
	"net/http"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
)

// HasPermission reports whether any of the staff member's role assignments
// grants the given permission, regardless of branch scope.
func HasPermission(claims *auth.StaffClaims, permission string) bool {
	allowed := allowedRoles(permission)
	if len(allowed) == 0 {
		return false
	}
	for _, ra := range claims.RoleAssignments {
		for _, role := range allowed {
			if ra.Role == role {
				return true
			}
		}
	}
	return false
}

// CanAccessBranch reports whether the staff member may act on the given
// branch. A brand-scoped role (super admin, finance) can access every branch;
// a branch-scoped role must match the branch exactly (RBAC-02: a user may hold different roles at different
// branches — scope is resolved per request, never cached as one global role).
func CanAccessBranch(claims *auth.StaffClaims, scopeIDs ...string) bool {
	branchID := ""
	if len(scopeIDs) > 0 {
		branchID = scopeIDs[len(scopeIDs)-1]
	}
	for _, ra := range claims.RoleAssignments {
		if isBranchScopedRole(ra.Role) {
			if ra.ScopeType == ScopeBranch && ra.BranchID != nil && *ra.BranchID == branchID {
				return true
			}
			continue
		}
		switch ra.ScopeType {
		case ScopeBrand:
			return true
		case ScopeBranch:
			if ra.BranchID != nil && *ra.BranchID == branchID {
				return true
			}
		}
	}
	return false
}

func isBranchScopedRole(role string) bool {
	switch role {
	case RoleBranchManager, RoleCounterStaff, RoleKitchen, RoleCentralKitchenStaff:
		return true
	default:
		return false
	}
}

// CentralKitchenScopes returns every central kitchen the caller is assigned
// to. A central-kitchen assignment grants nothing through CanAccessBranch —
// which is what keeps it off the POS and the admin routes — so the KDS
// resolves the stores behind it from the database instead.
func CentralKitchenScopes(claims *auth.StaffClaims) []string {
	if claims == nil {
		return nil
	}
	var out []string
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == ScopeCentralKitchen && ra.CentralKitchenID != nil && *ra.CentralKitchenID != "" {
			out = append(out, *ra.CentralKitchenID)
		}
	}
	return out
}

// BranchScopesForRole returns the stores the caller holds the given role at.
func BranchScopesForRole(claims *auth.StaffClaims, role string) []string {
	if claims == nil {
		return nil
	}
	var out []string
	for _, ra := range claims.RoleAssignments {
		if ra.Role == role && ra.ScopeType == ScopeBranch && ra.BranchID != nil && *ra.BranchID != "" {
			out = append(out, *ra.BranchID)
		}
	}
	return out
}

// OwnScope returns the branch a caller is confined to. The first return value
// is always empty: it used to carry a franchise, which migration 0070 removed,
// and is kept only so existing call sites (`_, branchID := OwnScope(...)`)
// read the same.
//
// An empty branch means "not confined" — a brand-wide assignment returns it,
// and so does a caller with no assignments at all, so check IsBrandScoped
// before reading it as "may see everything".
//
// Multi-branch staff (RBAC-02) hold several assignments; this returns the
// first, which is the default view. A screen wanting another branch names it
// explicitly and is checked against CanAccessBranch as usual.
func OwnScope(claims *auth.StaffClaims) (franchiseID, branchID string) {
	if claims == nil {
		return "", ""
	}
	for _, r := range claims.RoleAssignments {
		if r.ScopeType == ScopeBranch && r.BranchID != nil && *r.BranchID != "" && branchID == "" {
			branchID = *r.BranchID
		}
	}
	return franchiseID, branchID
}

// IsBrandScoped reports whether the staff member holds any brand-wide role
// (super admin, finance) — used for network dashboards and audit search.
func IsBrandScoped(claims *auth.StaffClaims) bool {
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == ScopeBrand {
			return true
		}
	}
	return false
}

// IsSuperAdmin reports whether the staff member holds the super_admin role.
func IsSuperAdmin(claims *auth.StaffClaims) bool {
	if claims == nil {
		return false
	}
	for _, ra := range claims.RoleAssignments {
		if ra.Role == RoleSuperAdmin {
			return true
		}
	}
	return false
}

// IsFinance reports whether the staff member holds the finance role.
func IsFinance(claims *auth.StaffClaims) bool {
	return HasRole(claims, RoleFinance)
}

// HasRole reports whether the staff member holds any of the given roles.
func HasRole(claims *auth.StaffClaims, roles ...string) bool {
	if claims == nil {
		return false
	}
	for _, ra := range claims.RoleAssignments {
		for _, r := range roles {
			if ra.Role == r {
				return true
			}
		}
	}
	return false
}

// RequirePermission is chi middleware that rejects the request unless the
// authenticated staff member holds the permission at ANY scope. Handlers
// that also need branch/franchise-level scope enforcement must additionally
// call CanAccessBranch once the target resource is known.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.StaffFromContext(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthenticated("Staff authentication required."))
				return
			}
			if !HasPermission(claims, permission) {
				httpx.WriteError(w, r, apperr.PermissionDenied("You do not have permission to perform this action."))
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}
