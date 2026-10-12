# API conventions

## Base URL

```
Production:   https://api.bakedbouquet.com/v1
Staging:      https://api-staging.bakedbouquet.com/v1
Local:        http://localhost:8080/v1
```

## Authentication

All authenticated endpoints require:
```
Authorization: Bearer <access_token>
```

Access tokens are short-lived (15 minutes). Refresh via `POST /v1/auth/refresh`.
Public endpoints (no auth): `/v1/auth/*`, `/v1/enquiries/*`, `/v1/content/*`,
`/v1/track/*`, `/.well-known/jwks.json`.

## Request format

```
Content-Type: application/json
Accept: application/json
```

## Response format

### Success

```json
{
  "data": { ... }
}
```

List responses:

```json
{
  "data": [...],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 234,
    "total_pages": 5
  }
}
```

### Error

```json
{
  "error": {
    "code": "SLOT_CAPACITY_EXCEEDED",
    "message": "No remaining capacity for the requested slot",
    "details": {
      "slot_date": "2026-09-05",
      "slot_start": "14:00"
    },
    "trace_id": "abc123",
    "retryable": false
  }
}
```

## Idempotency

Mutation endpoints that must be safe to retry require:
```
Idempotency-Key: <client-generated UUID>
```

Required on:
- `POST /v1/orders` — order creation
- `POST /v1/payments/...` — payment operations
- `POST /v1/orders/{id}/refund` — refund

If the same key is sent twice, the server returns the first response without
re-processing. Idempotency keys expire after 24 hours.

## Pagination

List endpoints accept:
```
?page=1&per_page=50
```

Default `per_page` is 50. Maximum is 200.

## Filtering

List endpoints accept filter params documented per endpoint. Common:
```
?status=placed
?from=2026-08-01&to=2026-08-31
?branch_id=uuid
?franchise_id=uuid
```

## Date and time formats

All timestamps: ISO 8601 with UTC timezone: `2026-09-05T14:00:00Z`.
All dates: `YYYY-MM-DD`.
All times (slots): `HH:MM` (24-hour, branch-local, communicated alongside timezone).

## Money

Always in minor units (cents) with explicit currency:
```json
{
  "amount_cents": 8800,
  "currency": "USD"
}
```

Display value (`$88.00`) is computed by the frontend.

## Error codes (partial list)

| Code | HTTP | Meaning |
|---|---|---|
| `UNAUTHORIZED` | 401 | Missing or invalid token |
| `FORBIDDEN` | 403 | Valid token but insufficient permissions |
| `NOT_FOUND` | 404 | Resource does not exist or is not visible to this token |
| `CONFLICT` | 409 | Unique constraint or state conflict |
| `VALIDATION_ERROR` | 422 | Request body failed validation |
| `SLOT_CAPACITY_EXCEEDED` | 409 | Requested slot is full |
| `DELIVERY_NOT_AVAILABLE` | 422 | No branch serves the requested ZIP |
| `DELIVERY_ZONE_OVERLAP` | 409 | Delivery zone conflicts with existing branch |
| `AMENDMENT_WINDOW_CLOSED` | 422 | Order is in production — no amendments |
| `IDEMPOTENCY_CONFLICT` | 409 | Same key, different request body |
| `RATE_LIMITED` | 429 | Too many requests |

Full list: `docs/api/error-catalog.md`.
