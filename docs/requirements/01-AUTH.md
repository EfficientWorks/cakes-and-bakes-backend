# Module 01 — Authentication & Customer Accounts

**Phase:** R1  
**BRD refs:** ACC-01 to ACC-17, RBAC-10, RBAC-11  
**Surfaces:** Customer storefront, customer account portal

---

## Overview

Two separate auth systems exist:
- **Customer auth** — email/password via Auth0, JWT tokens. Brand-level (not branch-level). One registration works across all branches.
- **Staff auth** — PIN-based shift login for POS and KDS (Phase 2). Separate from customer accounts and cannot be escalated.

---

## Database Tables

```sql
customers (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email           TEXT UNIQUE NOT NULL,
  auth0_id        TEXT UNIQUE,              -- null until verified
  first_name      TEXT NOT NULL,
  last_name       TEXT NOT NULL,
  mobile_number   TEXT,
  is_verified     BOOLEAN DEFAULT false,
  otp_code        TEXT,                     -- hashed, cleared on verify
  otp_expires_at  TIMESTAMPTZ,
  marketing_opt_in         BOOLEAN DEFAULT false,
  marketing_opt_in_at      TIMESTAMPTZ,
  marketing_opt_in_ip      TEXT,
  password_reset_token     TEXT,            -- hashed
  password_reset_expires   TIMESTAMPTZ,
  created_at      TIMESTAMPTZ DEFAULT NOW(),
  updated_at      TIMESTAMPTZ DEFAULT NOW(),
  deleted_at      TIMESTAMPTZ               -- soft delete only; orders retained anonymised
)

customer_addresses (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id     UUID REFERENCES customers(id),
  label           TEXT,                     -- e.g. "Home", "Work"
  street          TEXT NOT NULL,
  city            TEXT NOT NULL,
  state           TEXT NOT NULL,
  postcode        TEXT NOT NULL,
  country         TEXT DEFAULT 'US',
  is_default      BOOLEAN DEFAULT false,
  created_at      TIMESTAMPTZ DEFAULT NOW()
)

customer_recipients (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id     UUID REFERENCES customers(id),
  first_name      TEXT NOT NULL,
  last_name       TEXT NOT NULL,
  phone           TEXT NOT NULL,
  relationship    TEXT,
  occasion_dates  JSONB,                    -- [{occasion, month, day}]
  created_at      TIMESTAMPTZ DEFAULT NOW()
)
```

---

## API Endpoints

### Registration

#### `POST /api/v1/auth/register` — Step 1: create account
```
Body:
{
  "email": "customer@example.com",
  "password": "...",             // min 8 chars, validated server-side
  "confirmPassword": "..."
}

Response 201:
{
  "success": true,
  "data": { "message": "OTP sent to email. Please verify your account." }
}

Errors:
  409 EMAIL_ALREADY_EXISTS
  422 PASSWORD_MISMATCH
  422 PASSWORD_TOO_WEAK
```

#### `POST /api/v1/auth/register/profile` — Step 2: personal details
```
Body:
{
  "firstName": "Jane",
  "lastName":  "Smith",
  "mobileNumber": "+17325551234"
}

Headers: Authorization: Bearer <temp_token_from_step1>

Response 200:
{ "success": true, "data": { "message": "Profile saved. Complete address step." } }
```

#### `POST /api/v1/auth/register/address` — Step 3: address (optional but shown in Figma)
```
Body:
{
  "country":  "US",
  "state":    "NJ",
  "city":     "Edison",
  "postcode": "08837",
  "address":  "180 Lafayette Ave"
}

Response 200:
{ "success": true, "data": { "customerId": "...", "accessToken": "..." } }
```

---

### OTP Verification

#### `POST /api/v1/auth/verify-otp`
```
Body: { "email": "customer@example.com", "otp": "123456" }

Response 200:
{ "success": true, "data": { "accessToken": "...", "refreshToken": "..." } }

Errors:
  400 OTP_INVALID
  400 OTP_EXPIRED
```

#### `POST /api/v1/auth/resend-otp`
```
Body: { "email": "customer@example.com" }
Rate limited: 3 per 10 minutes per email

Response 200: { "success": true, "data": { "message": "OTP resent." } }
```

---

### Login

#### `POST /api/v1/auth/login`
```
Body: { "email": "...", "password": "..." }

Response 200:
{
  "success": true,
  "data": {
    "accessToken":  "...",   // 1 hour
    "refreshToken": "...",   // 30 days, rotated on use
    "customer": {
      "id":        "...",
      "firstName": "Jane",
      "lastName":  "Smith",
      "email":     "customer@example.com"
    }
  }
}

Errors:
  401 INVALID_CREDENTIALS
  403 ACCOUNT_NOT_VERIFIED
```

#### `POST /api/v1/auth/refresh`
```
Body: { "refreshToken": "..." }
Response 200: { "accessToken": "...", "refreshToken": "..." }
```

#### `POST /api/v1/auth/logout`
```
Headers: Authorization: Bearer <token>
Body: { "refreshToken": "..." }
Response 200: { "success": true }
```

---

### Password Reset

#### `POST /api/v1/auth/forgot-password`
```
Body: { "email": "customer@example.com" }
Response 200: { "success": true, "data": { "message": "Reset OTP sent to email." } }
(Always returns 200 even if email not found — prevents email enumeration)
```

#### `POST /api/v1/auth/verify-reset-otp`
```
Body: { "email": "...", "otp": "123456" }
Response 200: { "success": true, "data": { "resetToken": "..." } }
```

#### `POST /api/v1/auth/reset-password`
```
Body: { "resetToken": "...", "newPassword": "...", "confirmPassword": "..." }
Response 200: { "success": true, "data": { "message": "Password updated. Please log in." } }
```

---

## Customer Profile Endpoints

All require `Authorization: Bearer <token>`.

#### `GET /api/v1/account/profile`
```
Response 200:
{
  "id": "...",
  "firstName": "Jane",
  "lastName":  "Smith",
  "mobileNumber": "+17325551234",
  "email": "customer@example.com",
  "marketingOptIn": false,
  "createdAt": "2026-08-01T10:00:00Z"
}
```

#### `PATCH /api/v1/account/profile`
```
Body (all fields optional):
{
  "firstName":    "Jane",
  "lastName":     "Doe",
  "mobileNumber": "+17325551234"
}
Response 200: updated profile object
```

#### `POST /api/v1/account/change-password`
```
Body: { "currentPassword": "...", "newPassword": "...", "confirmPassword": "..." }
Response 200: { "success": true }
Errors: 401 WRONG_CURRENT_PASSWORD
```

#### `DELETE /api/v1/account` — Account deletion
```
Body: { "password": "..." }   // confirmation required

Behaviour:
- Anonymises PII in customers table (name → "Deleted User", email → deleted_{id}@removed.bb)
- Keeps order records with anonymised customer reference for tax/audit purposes
- Revokes all tokens

Response 200: { "success": true, "data": { "message": "Account deleted." } }
```

---

## Address Book Endpoints

#### `GET /api/v1/account/addresses`
```
Response 200: { "data": [ { "id", "label", "street", "city", "state", "postcode", "isDefault" } ] }
```

#### `POST /api/v1/account/addresses`
```
Body: { "label": "Home", "street": "...", "city": "Edison", "state": "NJ", "postcode": "08837" }
Response 201: address object
```

#### `PATCH /api/v1/account/addresses/:id`
```
Body: any address fields
Response 200: updated address object
```

#### `DELETE /api/v1/account/addresses/:id`
```
Response 200: { "success": true }
Errors: 400 CANNOT_DELETE_DEFAULT — must set another address as default first
```

#### `POST /api/v1/account/addresses/:id/set-default`
```
Response 200: { "success": true }
```

---

## Recipient Book Endpoints

#### `GET /api/v1/account/recipients`
```
Response 200: { "data": [ { "id", "firstName", "lastName", "phone", "relationship", "occasionDates" } ] }
```

#### `POST /api/v1/account/recipients`
```
Body:
{
  "firstName":    "Mary",
  "lastName":     "Jones",
  "phone":        "+17325559999",
  "relationship": "Sister",
  "occasionDates": [
    { "occasion": "Birthday", "month": 3, "day": 15 }
  ]
}
Response 201: recipient object
```

#### `PATCH /api/v1/account/recipients/:id`
#### `DELETE /api/v1/account/recipients/:id`

---

## Order History (Customer-facing)

#### `GET /api/v1/account/orders`
```
Query params:
  page=1, limit=10
  status=placed|fulfilled|cancelled

Response 200:
{
  "data": [
    {
      "id":          "EZYODR123",
      "placedAt":    "2026-08-02T12:30:00Z",
      "status":      "placed",
      "totalItems":  3,
      "total":       2900,           // cents
      "currency":    "USD",
      "branchName":  "Edison, NJ",
      "canCancel":   true
    }
  ],
  "meta": { "page": 1, "total": 12 }
}
```

#### `GET /api/v1/account/orders/:orderId`
```
Response 200: full order detail — see 06-ORDERS.md for shape
```

#### `POST /api/v1/account/orders/:orderId/cancel`
```
Body: { "reason": "Changed my mind" }

Pre-condition checks:
- Order is in cancellable state (PLACED or CONFIRMED, before cut-off)
- States refund outcome before confirming

Response 200:
{
  "success": true,
  "data": {
    "refundAmount": 7100,   // cents
    "refundMethod": "original_card",
    "message":      "Refund of $71.00 will appear in 5-10 business days."
  }
}
Errors:
  409 ORDER_IN_PRODUCTION — cannot cancel once kitchen has started
  409 ORDER_ALREADY_CANCELLED
```

#### `POST /api/v1/account/orders/:orderId/reorder`
```
Re-validates: current availability, pricing, branch still serves address

Response 200:
{
  "data": {
    "cartId":   "...",
    "warnings": [
      "Summer Sunset Bouquet is currently unavailable. Removed from cart."
    ]
  }
}
```

---

## Marketing Preferences

#### `PATCH /api/v1/account/marketing`
```
Body: { "optIn": true }

Stores: opt-in value + timestamp + IP address
Response 200: { "success": true }
```

---

## Guest Order Claim

#### `POST /api/v1/account/claim-order`
```
Body: { "orderReference": "EZYODR123", "email": "customer@example.com" }

Validates the email matches the purchaser email on the order.
Links the order to the authenticated customer account.

Response 200: { "success": true, "data": { "orderId": "..." } }
Errors:
  404 ORDER_NOT_FOUND
  409 ORDER_ALREADY_CLAIMED
  403 EMAIL_MISMATCH
```

---

## Token Structure (JWT Claims)

```json
{
  "sub":       "customer_uuid",
  "type":      "customer",
  "email":     "customer@example.com",
  "verified":  true,
  "iat":       1234567890,
  "exp":       1234571490
}
```

---

## Security Notes

- Passwords hashed with Argon2id
- OTP codes are 6-digit, stored hashed, expire in 10 minutes
- Reset tokens are 64-byte random hex, stored hashed, expire in 1 hour
- Rate limiting: login 5 attempts per 15 min per IP, OTP resend 3 per 10 min per email
- Email verification required before first order placement
- Customer accounts are brand-level — one registration across all branches (ACC-17)
