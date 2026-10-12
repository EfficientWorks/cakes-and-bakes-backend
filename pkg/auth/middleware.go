package auth

import (
	"context"
	"net/http"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
)

type ctxKey int

const (
	customerClaimsKey ctxKey = iota
	staffClaimsKey
)

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(h, "Bearer "), true
}

// RequireCustomer verifies a customer access token and injects its claims
// into the request context. Use OptionalCustomer for endpoints where guest
// checkout is allowed (SF-29) but a logged-in customer gets pre-filled data.
func RequireCustomer(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthenticated("Missing bearer token."))
				return
			}
			claims, err := issuer.ParseCustomerAccessToken(raw)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), customerClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalCustomer attaches customer claims to the context if a valid bearer
// token is present, but never rejects the request when it's absent.
func OptionalCustomer(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if raw, ok := bearerToken(r); ok {
				if claims, err := issuer.ParseCustomerAccessToken(raw); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), customerClaimsKey, claims))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireStaff verifies a staff access token and injects its claims into the
// request context.
func RequireStaff(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthenticated("Missing bearer token."))
				return
			}
			claims, err := issuer.ParseStaffAccessToken(raw)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), staffClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireStaffOrQueryToken is RequireStaff that also accepts the access token
// as an `accessToken` query parameter.
//
// Only for Server-Sent Events: a browser's EventSource cannot set an
// Authorization header, so a KDS running in a browser has no other way to
// authenticate its live stream. Every other route keeps header-only auth.
func RequireStaffOrQueryToken(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				raw = strings.TrimSpace(r.URL.Query().Get("accessToken"))
				ok = raw != ""
			}
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthenticated("Missing bearer token."))
				return
			}
			claims, err := issuer.ParseStaffAccessToken(raw)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), staffClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CustomerFromContext returns the authenticated customer's claims, if any.
func CustomerFromContext(ctx context.Context) (*CustomerClaims, bool) {
	c, ok := ctx.Value(customerClaimsKey).(*CustomerClaims)
	return c, ok
}

// StaffFromContext returns the authenticated staff member's claims, if any.
func StaffFromContext(ctx context.Context) (*StaffClaims, bool) {
	c, ok := ctx.Value(staffClaimsKey).(*StaffClaims)
	return c, ok
}
