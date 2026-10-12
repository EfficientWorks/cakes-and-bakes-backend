package auth

import (
	"testing"
	"time"
)

func TestCustomerTokenRoundTrip(t *testing.T) {
	issuer := NewIssuer("test-secret", "emzo-bakehouse")

	tok, exp, err := issuer.IssueCustomerAccessToken("cust-1", "jane@example.com", true, time.Hour)
	if err != nil {
		t.Fatalf("IssueCustomerAccessToken: %v", err)
	}
	if exp.Before(time.Now()) {
		t.Fatal("expiry should be in the future")
	}

	claims, err := issuer.ParseCustomerAccessToken(tok)
	if err != nil {
		t.Fatalf("ParseCustomerAccessToken: %v", err)
	}
	if claims.CustomerID != "cust-1" || claims.Email != "jane@example.com" || !claims.Verified {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestParseCustomerAccessTokenRejectsWrongSecret(t *testing.T) {
	issuer := NewIssuer("secret-a", "emzo-bakehouse")
	tok, _, err := issuer.IssueCustomerAccessToken("cust-1", "jane@example.com", true, time.Hour)
	if err != nil {
		t.Fatalf("IssueCustomerAccessToken: %v", err)
	}

	other := NewIssuer("secret-b", "emzo-bakehouse")
	if _, err := other.ParseCustomerAccessToken(tok); err == nil {
		t.Fatal("expected token signed with a different secret to be rejected")
	}
}

func TestStaffTokenCarriesRoleAssignments(t *testing.T) {
	issuer := NewIssuer("test-secret", "emzo-bakehouse")
	roles := []RoleAssignment{
		{Role: "branch_manager", ScopeType: "branch", BranchID: strPtr("br-1")},
		{Role: "franchise_owner", ScopeType: "branch", BranchID: strPtr("br-2")},
	}

	tok, _, err := issuer.IssueStaffAccessToken("staff-1", "staff@example.com", roles, time.Hour)
	if err != nil {
		t.Fatalf("IssueStaffAccessToken: %v", err)
	}

	claims, err := issuer.ParseStaffAccessToken(tok)
	if err != nil {
		t.Fatalf("ParseStaffAccessToken: %v", err)
	}
	if len(claims.RoleAssignments) != 2 {
		t.Fatalf("expected 2 role assignments, got %d", len(claims.RoleAssignments))
	}
}

func strPtr(s string) *string { return &s }
