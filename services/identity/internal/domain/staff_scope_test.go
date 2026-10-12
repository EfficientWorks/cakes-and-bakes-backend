package domain

import (
	"errors"
	"strings"
	"testing"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
)

func ptr(s string) *string { return &s }

// The scope pairing the database enforces, enforced here first so the caller
// is told which field is wrong instead of "an unexpected error occurred".
func TestValidateRoleScope(t *testing.T) {
	cases := []struct {
		name    string
		role    identityapi.StaffRoleAssignment
		wantErr string
	}{
		{
			name: "brand scope needs no ids",
			role: identityapi.StaffRoleAssignment{Role: tenant.RoleSuperAdmin, ScopeType: tenant.ScopeBrand},
		},
		{
			name:    "brand scope rejects a branch",
			role:    identityapi.StaffRoleAssignment{Role: tenant.RoleSuperAdmin, ScopeType: tenant.ScopeBrand, BranchID: ptr("b1")},
			wantErr: "whole network",
		},
		{
			// Retired in migration 0070: a franchise-scoped row would now
			// grant access to nothing, so it is refused up front.
			name:    "franchise scope is no longer accepted",
			role:    identityapi.StaffRoleAssignment{Role: tenant.RoleFranchiseOwner, ScopeType: tenant.ScopeFranchise},
			wantErr: "no longer supported",
		},
		{
			name: "franchise owner is accepted at a branch",
			role: identityapi.StaffRoleAssignment{Role: tenant.RoleFranchiseOwner, ScopeType: tenant.ScopeBranch, BranchID: ptr("b1")},
		},
		{
			name:    "branch scope needs a branch",
			role:    identityapi.StaffRoleAssignment{Role: tenant.RoleBranchManager, ScopeType: tenant.ScopeBranch},
			wantErr: "which outlet",
		},
		{
			name: "branch scope accepts a branch",
			role: identityapi.StaffRoleAssignment{Role: tenant.RoleBranchManager, ScopeType: tenant.ScopeBranch, BranchID: ptr("b1")},
		},
		{
			name:    "an unknown role is refused",
			role:    identityapi.StaffRoleAssignment{Role: "wizard", ScopeType: tenant.ScopeBrand},
			wantErr: "is not a role",
		},
		{
			name:    "a missing role is refused",
			role:    identityapi.StaffRoleAssignment{ScopeType: tenant.ScopeBrand},
			wantErr: "A role is required",
		},
		{
			name:    "a missing scope is refused",
			role:    identityapi.StaffRoleAssignment{Role: tenant.RoleKitchen},
			wantErr: "brand or one outlet",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRoleScope(c.role)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("wanted this to be accepted, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("wanted an error containing %q, got none", c.wantErr)
			}
			var appErr *apperr.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("error is not an *apperr.Error: %v", err)
			}
			if appErr.Category != apperr.CategoryValidation {
				t.Errorf("category = %q, want a validation failure so the caller sees a 400",
					appErr.Category)
			}
			if !strings.Contains(strings.ToLower(appErr.Message), strings.ToLower(c.wantErr)) {
				t.Errorf("message = %q, want it to mention %q", appErr.Message, c.wantErr)
			}
			if len(appErr.Fields) == 0 {
				t.Errorf("no field was named, so the form cannot show the error against an input")
			}
		})
	}
}

// Every role the console can offer must be one the API accepts.
func TestEveryKnownRoleIsAccepted(t *testing.T) {
	for _, role := range tenant.AllRoles() {
		err := validateRoleScope(identityapi.StaffRoleAssignment{
			Role: role, ScopeType: tenant.ScopeBranch, BranchID: ptr("b1"),
		})
		if err != nil {
			t.Errorf("role %q was refused: %v", role, err)
		}
	}
}

// An outlet-level manager must not be able to hand out roles above their own
// station — the update path used to skip this check entirely.
func TestMayGrantCeiling(t *testing.T) {
	manager := &auth.StaffClaims{RoleAssignments: []auth.RoleAssignment{
		{Role: tenant.RoleBranchManager, ScopeType: tenant.ScopeBranch, BranchID: ptr("b1")},
	}}
	cases := []struct {
		name    string
		role    identityapi.StaffRoleAssignment
		allowed bool
	}{
		{"super admin", identityapi.StaffRoleAssignment{Role: tenant.RoleSuperAdmin, ScopeType: tenant.ScopeBrand}, false},
		{"brand-wide finance", identityapi.StaffRoleAssignment{Role: tenant.RoleFinance, ScopeType: tenant.ScopeBrand}, false},
		{"counter staff at another outlet", identityapi.StaffRoleAssignment{Role: tenant.RoleCounterStaff, ScopeType: tenant.ScopeBranch, BranchID: ptr("b2")}, false},
		{"counter staff at own outlet", identityapi.StaffRoleAssignment{Role: tenant.RoleCounterStaff, ScopeType: tenant.ScopeBranch, BranchID: ptr("b1")}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mayGrant(manager, c.role)
			if c.allowed && err != nil {
				t.Fatalf("wanted allowed, got %v", err)
			}
			if !c.allowed && err == nil {
				t.Fatal("wanted refused, got allowed")
			}
		})
	}
}
