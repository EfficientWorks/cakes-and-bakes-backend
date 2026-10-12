# Authentication

## Two consumer realms + one support realm

| Realm | Who | Login method | Token lifetime |
|---|---|---|---|
| `customer` | Storefront customers | Email + password | Access: 15min, Refresh: 30 days |
| `staff` | All staff roles | Email + password (admin) or PIN (terminal) | Access: 15min, Refresh: 24h |
| `support` | Super admin, platform support | Email + password (global DB only) | Access: 15min, Refresh: 8h |

Customer and staff accounts are completely isolated. A customer account cannot be
used to log in as staff. A staff account cannot browse the storefront as a customer.

## JWT structure

EdDSA (Ed25519) signed JWTs. Short-lived access tokens (15 min). Rotating refresh tokens
stored as argon2id hash in Postgres (not in Redis at launch).

### Customer access token claims

```json
{
  "sub": "customer-uuid",
  "realm": "customer",
  "email": "jane@example.com",
  "iat": 1722800000,
  "exp": 1722800900
}
```

### Staff access token claims

```json
{
  "sub": "staff-user-uuid",
  "realm": "staff",
  "franchise_id": "franchise-uuid",
  "branch_id": "branch-uuid",
  "role": "branch_manager",
  "region": "us",
  "permissions": ["orders.read", "orders.amend", "catalog.availability.toggle"],
  "limits": {
    "max_refund_cents": 5000,
    "max_discount_pct": 20,
    "void_window_minutes": 30
  },
  "iat": 1722800000,
  "exp": 1722800900
}
```

Permissions are resolved at login from the role's default permission set, modified by any
per-user overrides. They are embedded in the token to avoid a DB lookup on every request.
Permissions are **re-resolved** on every token refresh — stale permissions are never
extended beyond 15 minutes.

### Support access token claims

```json
{
  "sub": "super-admin-uuid",
  "realm": "support",
  "role": "super_admin",
  "iat": 1722800000,
  "exp": 1722800900
}
```

No `franchise_id` or `branch_id`. The `support` realm bypasses per-permission checks
entirely (see `pkg/auth.RequirePermission`).

## JWKS

`GET /.well-known/jwks.json` — public keys for token verification.
All internal services verify tokens against this endpoint, cached with 5-minute TTL.
No service calls back to `identity` on every request.

## Customer registration flow

```
1. POST /v1/auth/customer/register  {email, password}
   → create unverified customer_accounts row
   → generate 6-digit OTP, store hash in email_verification_tokens
   → send OTP email via Postmark
   → return {message: "Verification email sent"}

2. POST /v1/auth/customer/verify-email  {email, otp}
   → verify OTP hash, mark email_verified_at
   → delete token row
   → return {access_token, refresh_token}
```

Figma shows 3-step signup (email+password → name+phone → address). Steps 2 and 3 are
profile updates after the account exists, not part of auth. They use `PATCH /v1/customer/profile`.

## Terminal (PIN) login

```
POST /v1/auth/terminal/login
{
  "branch_registry_id": "BB-NJ-001",
  "pin": "1234",
  "app": "pos"       // "pos" | "kds"
}
```

Steps:
1. Resolve `branch_registry_id` → `branch_id`, `franchise_id`, `region` (from global DB).
2. Load `staff_users` for the branch in the regional DB.
3. Indexed lookup via `pin_lookup` (HMAC-SHA256 of PIN) → candidate user in O(1).
4. argon2id-verify full `pin_hash` against the submitted PIN.
5. Check role is allowed for the requested `app`.
   - `kds`: only `kitchen_staff`
   - `pos`: `counter_staff`, `branch_manager`
6. Return `{access_token, refresh_token}` with `realm: staff`.

## Refresh flow

```
POST /v1/auth/refresh
{
  "refresh_token": "opaque-token"
}
```

Server:
1. Hash the submitted token, look up in `*_sessions` table.
2. Verify not expired. Verify `revoked_at` is null.
3. **Re-resolve permissions** from current DB state (not copy from old token).
4. Revoke the old refresh token.
5. Issue new `{access_token, refresh_token}`.

Reuse detection: if a refresh token that was already revoked is submitted, all tokens for
that user are immediately revoked (potential token theft). User must re-login.

## Permission resolution

```go
// pkg/auth/middleware.go

func RequirePermission(perm string) func(http.Handler) http.Handler {
  return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      claims := auth.ClaimsFromContext(r.Context())

      // Support realm bypasses all permission checks
      if claims.Realm == RealmSupport {
        next.ServeHTTP(w, r)
        return
      }

      // Check permission in token claims
      if !claims.Has(perm) {
        httpx.WriteError(w, apperr.New("FORBIDDEN", "Insufficient permissions", 403))
        return
      }

      next.ServeHTTP(w, r)
    })
  }
}
```

## Numeric limits

Per-user limits are embedded in the JWT claims and enforced server-side:

```go
func RequireLimit(limitKey string, valueExtractor func(*http.Request) int64) func(http.Handler) http.Handler {
  return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      claims := auth.ClaimsFromContext(r.Context())
      limit, ok := claims.Limits[limitKey]
      if ok {
        value := valueExtractor(r)
        if value > limit {
          httpx.WriteError(w, apperr.New("LIMIT_EXCEEDED",
            fmt.Sprintf("Amount exceeds your limit of %d", limit), 403))
          return
        }
      }
      next.ServeHTTP(w, r)
    })
  }
}
```
