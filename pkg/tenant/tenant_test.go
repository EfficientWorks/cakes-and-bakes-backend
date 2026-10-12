package tenant

import (
	"testing"

	"bakehouse/pkg/auth"
)

func TestHasPermissionRespectsMatrix(t *testing.T) {
	branchManager := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{{Role: RoleBranchManager, ScopeType: ScopeBranch, BranchID: strPtr("br-1")}}}
	kitchen := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{{Role: RoleKitchen, ScopeType: ScopeBranch, BranchID: strPtr("br-1")}}}

	if !HasPermission(branchManager, PermOrdersRefundFull) {
		t.Error("branch manager should be able to issue a full refund")
	}
	if HasPermission(kitchen, PermOrdersRefundFull) {
		t.Error("kitchen role must never be able to issue refunds (RBAC-09)")
	}
	if !HasPermission(kitchen, PermKitchenMarkStatus) {
		t.Error("kitchen role should be able to mark production status")
	}
}

func TestCanAccessBranchScopeRules(t *testing.T) {
	branchID := "br-1"

	brandUser := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{{Role: RoleSuperAdmin, ScopeType: ScopeBrand}}}
	branchOwner := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{{Role: RoleFranchiseOwner, ScopeType: ScopeBranch, BranchID: &branchID}}}
	branchUser := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{{Role: RoleBranchManager, ScopeType: ScopeBranch, BranchID: &branchID}}}
	unrelatedBranchUser := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{{Role: RoleBranchManager, ScopeType: ScopeBranch, BranchID: strPtr("br-2")}}}

	if !CanAccessBranch(brandUser, branchID) {
		t.Error("brand-scoped role must access every branch")
	}
	if !CanAccessBranch(branchOwner, branchID) {
		t.Error("branch-scoped role must access its own branch")
	}
	if !CanAccessBranch(branchUser, branchID) {
		t.Error("branch-scoped role must access its own branch")
	}
	if CanAccessBranch(unrelatedBranchUser, branchID) {
		t.Error("branch-scoped role must NOT access a different branch (RBAC-06 data isolation)")
	}
}

func strPtr(s string) *string { return &s }
