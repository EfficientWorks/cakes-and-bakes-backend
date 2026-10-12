# Identity module

Service: `services/identity/`. Phase 1.

## What this module owns

- **Customer auth** — registration, email verification, login, password reset.
- **Staff auth** — admin login (email+password), terminal login (PIN).
- **Support auth** — super admin login (global DB only).
- **RBAC** — roles, permissions, permission overrides, numeric limits.
- **JWKS** — public key endpoint for all other services to verify tokens.
- **Sessions** — refresh token lifecycle, reuse detection.

## Two realms, completely isolated

```
customer realm   → customer_accounts, customer_sessions
staff realm      → staff_users, staff_login_sessions
support realm    → super_admin_users, support_sessions (global DB)
```

A customer account cannot be escalated into a staff account. JWT `realm` claim
distinguishes them — every protected endpoint checks realm before permissions.

## Role → access mapping (permission matrix)

See `docs/modules/identity/rbac.md` for the complete matrix.

Summary:

| Role | Franchise scope | Branch scope | Key capabilities |
|---|---|---|---|
| `super_admin` | All | All | Everything |
| `finance` | All | All | Reports only, no mutations |
| `franchise_owner` | Own | Own | Operational control + staff management |
| `franchise_viewer` | Own | Own | Sales data only |
| `branch_manager` | — | Own | Day-to-day ops, amendments, refunds up to limit |
| `counter_staff` | — | Own | POS: orders, payment |
| `kitchen_staff` | — | Own | KDS only — no pricing/payment data |

## Terminal PIN login (POS/KDS)

PIN is stored as two fields:
- `pin_hash` — full argon2id hash (for verification)
- `pin_lookup` — HMAC-SHA256(PIN, lookup_secret) — indexed for fast lookup

Login: indexed scan on `pin_lookup` finds candidate in O(1), then argon2id-verify
against `pin_hash`. App-access gating rejects `kitchen_staff` from POS, etc.

## See also

- `docs/auth/authentication.md` — token format, refresh flow, realm isolation
- `docs/phases/phase1-identity.md` — full schema and API
- `docs/modules/identity/rbac.md` — permission matrix
