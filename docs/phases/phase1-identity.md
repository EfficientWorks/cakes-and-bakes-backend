# Phase 1 — Identity

Service: `services/identity/`. See `docs/modules/identity/README.md`.

Two completely isolated auth realms:
- **Customer realm**: storefront account holders.
- **Staff realm**: super admin, franchise owner, branch manager, counter staff, kitchen staff.
- **Support realm** (region-less): platform-level super admin access.

## 1.1 Global DB migrations

`services/identity/db/migrations/global/`:

```sql
-- super admin and support users
super_admin_users       (id, email, password_hash, full_name, status, created_at)
support_sessions        (id, user_id, token_hash, expires_at, created_at)
```

## 1.2 Regional DB migrations

`services/identity/db/migrations/regional/`:

```sql
-- Staff (all franchise/branch roles)
staff_users (
  id, franchise_id, branch_id, email, password_hash, pin_hash, pin_lookup,
  full_name, role, status, last_login_at, created_at, updated_at
)

-- Roles and permissions (seeded, not user-created)
roles                   (id, key, name, realm, is_system)
permissions             (id, key, name, module, description)
role_permissions        (role_id, permission_id)
permission_overrides    (user_id, permission_id, effect, expires_at) -- grant/deny per user
numeric_limits          (user_id, limit_key, limit_value)            -- e.g. max_refund_cents

-- Customer accounts (separate realm)
customer_accounts (
  id, email, email_verified_at, password_hash, first_name, last_name,
  mobile, marketing_opt_in, marketing_opt_in_at, status, created_at, updated_at
)
email_verification_tokens   (id, customer_id, token_hash, expires_at, used_at)
password_reset_tokens        (id, customer_id, token_hash, expires_at, used_at)
customer_sessions            (id, customer_id, refresh_token_hash, expires_at, created_at)

-- Staff sessions
staff_login_sessions    (id, user_id, refresh_token_hash, realm, expires_at, created_at)
```

## 1.3 Staff roles (seeded)

9 roles for BB context:

| Role key | Level | Default access |
|---|---|---|
| `super_admin` | Brand (global) | Full network control |
| `finance` | Brand | Reports and exports, read-only |
| `franchise_owner` | Franchise | Operational control of own branches |
| `franchise_viewer` | Franchise | Sales data only, no operational control |
| `branch_manager` | Branch | Day-to-day operations |
| `counter_staff` | Branch | POS — take orders, process payment |
| `kitchen_staff` | Branch | KDS only — no pricing/payment data |

## 1.4 Login endpoints

### `POST /v1/auth/customer/register`
`{email, password}` → creates unverified customer account, sends OTP email.

### `POST /v1/auth/customer/verify-email`
`{email, otp}` → verifies email, returns `{access_token, refresh_token}`.

### `POST /v1/auth/customer/login`
`{email, password}` → `{access_token, refresh_token}`. Realm: `customer`.

### `POST /v1/auth/customer/forgot-password`
`{email}` → sends reset token email.

### `POST /v1/auth/customer/reset-password`
`{token, new_password}` → updates hash, invalidates all sessions.

### `POST /v1/auth/staff/login`
`{email, password}` → resolves franchise + branch → `{access_token, refresh_token}`.
Realm: `staff`. Claims carry `franchise_id`, `branch_id`, `role`, resolved permissions.

### `POST /v1/auth/support/login`
`{email, password}` → global DB only → `{access_token, refresh_token}`.
Realm: `support`. No `franchise_id` or `branch_id` in claims.

### `POST /v1/auth/terminal/login` (Phase 2 — POS/KDS)
`{branch_registry_id, pin, app: "pos"|"kds"}` → pin lookup index → argon2id verify →
`{access_token, refresh_token}`. Realm: `staff`.

### `POST /v1/auth/refresh`
`{refresh_token}` → re-resolves permissions from current DB state (not copying old claims)
→ new `{access_token, refresh_token}` pair. Old refresh token invalidated (rotation).

## 1.5 JWKS

`GET /.well-known/jwks.json` — EdDSA public key. All other services verify tokens here.
`pkg/auth.RemoteKeySet` handles caching with TTL.

## 1.6 RBAC

See `docs/modules/identity/rbac.md` for full permission matrix.

- Layer 1: module access per role (which nav sections are visible)
- Layer 2: action-level permissions (what the user can do)
- Layer 3: numeric limits (max refund value, max discount %, void window)

Resolution order: role defaults → user overrides (grant/deny) → numeric limits.
Cached in JWT claims at login. Re-resolved on every refresh (not in every API call).
Server re-authorises every mutation via `pkg/auth.RequirePermission` middleware.

## 1.7 Customer account endpoints

```
GET    /v1/customer/profile               → own profile
PATCH  /v1/customer/profile               → update name, mobile
POST   /v1/customer/profile/change-password
DELETE /v1/customer/account               → anonymise (GDPR — does not delete order records)
```

## 1.8 Swagger + tests

`services/identity/api/openapi.yaml` — all endpoints.
`postman/identity.postman_collection.json`.
Unit tests: hashing, permission resolution, OTP expiry, refresh rotation.
Integration tests: full register → verify → login round-trip. Staff login round-trip.

## Previous / Next

← [Phase 0 — Foundation](phase0-foundation.md) · [Phase 2 — Franchise](phase2-franchise.md) →
