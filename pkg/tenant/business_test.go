package tenant

import (
	"context"
	"testing"

	"bakehouse/pkg/auth"
)

func TestOwnsRow(t *testing.T) {
	a, b := "biz-a", "biz-b"
	system := context.Background()
	scopedA := WithBusiness(system, a)
	legacy := WithBusiness(system, "")

	cases := []struct {
		name string
		ctx  context.Context
		row  *string
		want bool
	}{
		{"system work reaches any row", system, &b, true},
		{"same business", scopedA, &a, true},
		{"other business", scopedA, &b, false},
		{"business request cannot see pre-business rows", scopedA, nil, false},
		{"pre-business account sees pre-business rows", legacy, nil, true},
		{"pre-business account cannot see a business's rows", legacy, &a, false},
	}
	for _, c := range cases {
		if got := OwnsRow(c.ctx, c.row); got != c.want {
			t.Errorf("%s: OwnsRow = %v, want %v", c.name, got, c.want)
		}
	}
	if BusinessParam(system) != nil || *BusinessParam(scopedA) != a {
		t.Fatal("BusinessParam is wrong")
	}
}

func TestBrandRoleReachesOnlyOwnBusinessStores(t *testing.T) {
	SetBranchBusinessLookup(func(branchID string) (string, bool, error) {
		switch branchID {
		case "store-a":
			return "biz-a", true, nil
		case "store-b":
			return "biz-b", true, nil
		}
		return "", false, nil
	})
	t.Cleanup(func() { SetBranchBusinessLookup(nil) })

	owner := &auth.StaffClaims{BusinessID: "biz-a", RoleAssignments: []auth.RoleAssignment{{Role: RoleSuperAdmin, ScopeType: ScopeBrand}}}
	if !CanAccessBranch(owner, "", "store-a") {
		t.Error("owner should reach their own store")
	}
	if CanAccessBranch(owner, "", "store-b") {
		t.Error("owner must not reach another business's store")
	}
	if CanAccessBranch(owner, "", "store-missing") {
		t.Error("an unknown store must be refused")
	}
}
