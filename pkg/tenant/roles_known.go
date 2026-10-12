package tenant

// AllRoles is every role the permission matrix defines, in seniority order.
//
// One list, so the API's validation, the console's picker and the matrix
// cannot drift apart — a role accepted by one and unknown to another is an
// account that exists and can do nothing.
func AllRoles() []string {
	return []string{
		RoleSuperAdmin,
		RoleFinance,
		RoleFranchiseOwner,
		RoleFranchiseViewer,
		RoleBranchManager,
		RoleCounterStaff,
		RoleKitchen,
		RoleCentralKitchenStaff,
	}
}

// IsKnownRole reports whether a string names a role.
func IsKnownRole(role string) bool {
	for _, r := range AllRoles() {
		if r == role {
			return true
		}
	}
	return false
}
