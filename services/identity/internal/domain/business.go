package domain

import "bakehouse/pkg/auth"

// claimsBusinessID is the business the caller belongs to, or "" for an
// account created before businesses existed.
func claimsBusinessID(claims *auth.StaffClaims) string {
	if claims == nil {
		return ""
	}
	return claims.BusinessID
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
