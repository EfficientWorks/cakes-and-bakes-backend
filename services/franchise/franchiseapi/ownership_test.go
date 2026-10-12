package franchiseapi

import "testing"

func TestNormalizeOwnershipType(t *testing.T) {
	cases := map[string]string{
		"corporate_store": OwnershipCorporate,
		"corporate store": OwnershipCorporate, // pre-0072 value
		"Corporate-Store": OwnershipCorporate,
		"own":             OwnershipCorporate, // storefront filter
		"franchise_store": OwnershipFranchise,
		"franchise store": OwnershipFranchise,
		"franchise":       OwnershipFranchise,
	}
	for in, want := range cases {
		got, ok := NormalizeOwnershipType(in)
		if !ok || got != want {
			t.Errorf("NormalizeOwnershipType(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "kiosk", "retail"} {
		if _, ok := NormalizeOwnershipType(bad); ok {
			t.Errorf("NormalizeOwnershipType(%q) was accepted", bad)
		}
	}
}
