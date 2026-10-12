package tenant

import (
	"testing"

	"bakehouse/pkg/auth"
)

func staff(roles ...string) *auth.StaffClaims {
	c := &auth.StaffClaims{StaffID: "s1"}
	for _, r := range roles {
		c.RoleAssignments = append(c.RoleAssignments, auth.RoleAssignment{Role: r, ScopeType: ScopeBrand})
	}
	return c
}

// The two view-only roles must not hold any permission that writes.
//
// This is the shape of the bug that let finance and franchise_viewer mark
// orders fulfilled: a write gated on a read permission. Pinning the matrix
// itself means the next such mistake shows up here rather than in production.
func TestViewOnlyRolesHoldNoWritePermissions(t *testing.T) {
	writes := []string{
		PermOrdersAmend,
		PermOrdersCancel,
		PermOrdersRefundLimited,
		PermOrdersCreateManual,
		PermOrdersReassign,
		PermCatalogueEditMaster,
		PermCatalogueAvailability,
		PermCatalogueSoldOut,
		PermStaffManage,
		PermFranchiseManage,
		PermFranchiseSuspend,
		PermBranchManage,
		PermBranchOperational,
		PermBranchCommercial,
	}

	for _, role := range []string{RoleFranchiseViewer, RoleFinance} {
		for _, perm := range writes {
			if HasPermission(staff(role), perm) {
				t.Errorf("%s holds the write permission %s", role, perm)
			}
		}
	}
}

// Counter staff may refund part of an order but not all of it.
func TestCounterStaffCannotRefundInFull(t *testing.T) {
	cs := staff(RoleCounterStaff)
	if !HasPermission(cs, PermOrdersRefundLimited) {
		t.Error("counter_staff lost the limited refund permission")
	}
	if HasPermission(cs, PermOrdersRefundFull) {
		t.Error("counter_staff holds the full refund permission")
	}
}

// Kitchen sees its own queue and nothing else — no customers, no money, no
// order list.
func TestKitchenIsConfinedToProduction(t *testing.T) {
	k := staff(RoleKitchen)
	for _, perm := range []string{
		PermOrdersView, PermOrdersAmend, PermOrdersRefundLimited,
		PermCustomersView, PermStaffView, PermReportsNetwork,
	} {
		if HasPermission(k, perm) {
			t.Errorf("kitchen holds %s", perm)
		}
	}
	if !HasPermission(k, PermKitchenViewOrders) {
		t.Error("kitchen cannot see the kitchen queue")
	}
}

// Only the brand-level super admin edits the master catalogue or changes the
// shape of the network.
func TestBrandOnlyPermissions(t *testing.T) {
	brandOnly := []string{PermCatalogueEditMaster, PermFranchiseManage, PermFranchiseSuspend}

	for _, perm := range brandOnly {
		if !HasPermission(staff(RoleSuperAdmin), perm) {
			t.Errorf("super_admin lacks %s", perm)
		}
		for _, role := range []string{
			RoleFinance, RoleFranchiseOwner, RoleFranchiseViewer,
			RoleBranchManager, RoleCounterStaff, RoleKitchen,
		} {
			if HasPermission(staff(role), perm) {
				t.Errorf("%s holds the brand-only permission %s", role, perm)
			}
		}
	}
}

// An unknown permission grants nothing — a typo must deny, never allow.
func TestUnknownPermissionDenies(t *testing.T) {
	if HasPermission(staff(RoleSuperAdmin), "orders.does_not_exist") {
		t.Error("an undeclared permission was granted")
	}
}

// Every role in the matrix is one of the eight declared roles (seven, plus
// central_kitchen_staff for the KDS): a typo in a
// grant list would otherwise silently grant nothing to a role that does not
// exist.
func TestMatrixReferencesOnlyRealRoles(t *testing.T) {
	real := map[string]bool{}
	for _, r := range permAll {
		real[r] = true
	}
	if len(real) != 8 {
		t.Fatalf("expected 8 roles, found %d", len(real))
	}

	for perm, roles := range matrix {
		if len(roles) == 0 {
			t.Errorf("%s grants nothing to anyone", perm)
		}
		for _, r := range roles {
			if !real[r] {
				t.Errorf("%s grants to unknown role %q", perm, r)
			}
		}
	}
}

// The two refund permissions are independent grants, not a ladder.
//
// finance holds the full one without the limited one; counter_staff the
// reverse. Code that treats either as implying the other locks one of them out
// of refunds it is entitled to make.
func TestRefundPermissionsAreIndependent(t *testing.T) {
	cases := []struct {
		role                string
		canPartial, canFull bool
	}{
		{RoleSuperAdmin, true, true},
		{RoleFranchiseOwner, true, true},
		{RoleBranchManager, true, true},
		{RoleCounterStaff, true, false}, // part of an order only
		{RoleFinance, false, true},      // the whole order only
		{RoleFranchiseViewer, false, false},
		{RoleKitchen, false, false},
	}

	for _, c := range cases {
		t.Run(c.role, func(t *testing.T) {
			if got := HasPermission(staff(c.role), PermOrdersRefundLimited); got != c.canPartial {
				t.Errorf("refund_limited = %v, want %v", got, c.canPartial)
			}
			if got := HasPermission(staff(c.role), PermOrdersRefundFull); got != c.canFull {
				t.Errorf("refund_full = %v, want %v", got, c.canFull)
			}
		})
	}
}
