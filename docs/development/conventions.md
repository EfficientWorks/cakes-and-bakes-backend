# Conventions

## Layout

```
bb-server/
├── pkg/                         ← shared code (all services can import)
│   ├── apperr/                  ← error types and codes
│   ├── auth/                    ← JWT, JWKS, middleware
│   ├── config/                  ← config loading
│   ├── db/                      ← pgxpool, migrations, regional pool manager
│   ├── email/                   ← Postmark client
│   ├── events/                  ← outbox publisher interface
│   ├── flags/                   ← feature flags
│   ├── httpx/                   ← router, JSON helpers, error writer
│   ├── obs/                     ← slog-based structured logging
│   ├── tenant/                  ← scope derivation from JWT
│   └── testutil/                ← test helpers
│
├── services/
│   ├── identity/
│   │   ├── cmd/server/main.go
│   │   ├── api/openapi.yaml
│   │   ├── db/
│   │   │   ├── migrations/global/
│   │   │   ├── migrations/regional/
│   │   │   └── queries/
│   │   └── internal/
│   │       ├── api/             ← HTTP handlers
│   │       ├── domain/          ← business logic
│   │       └── store/           ← sqlc-generated queries
│   │
│   ├── franchise/               ← same structure
│   ├── catalog/
│   ├── orders/
│   ├── scheduling/
│   ├── payments/
│   ├── admin/
│   ├── realtime/
│   └── gateway/
│
├── proto/                       ← ConnectRPC proto files per service
├── gen/                         ← generated protobuf code (committed)
├── postman/                     ← Postman collections per service
├── infra/docker/                ← docker-compose for local dev
├── docs/                        ← all documentation
└── scripts/
```

## Errors

Return `*apperr.Error` with a stable `UPPER_SNAKE` code. Render via `httpx.WriteError`.
Never leak internal detail or stack traces to clients. Never log PII.

```go
// Correct
return nil, apperr.New("SLOT_CAPACITY_EXCEEDED",
  "No remaining capacity for the requested slot",
  http.StatusConflict)

// Never
return nil, fmt.Errorf("slot inventory query failed: %w", err)
```

Error codes are single-sourced in `pkg/apperr/codes.go`. Add new codes there, not inline.

## Logging

`slog` via `pkg/obs`. Log once at the request boundary. Never inside domain functions.

```go
// Correct — log at the handler boundary
slog.ErrorContext(ctx, "order creation failed",
  "error", err.Error(),
  "order_id", req.OrderID)

// Never — log inside domain/service
func (s *Service) CreateOrder(...) {
  slog.Info("creating order", ...)  // never
}
```

## Tenancy

Derive scope from JWT via `pkg/tenant`. Never from URL or request body.

```go
scope := tenant.FromContext(ctx)
// scope.FranchiseID, scope.BranchID, scope.Realm, scope.Role
```

## Money

Integer minor units + currency. Never floats.

```go
// Correct
type Price struct {
  AmountCents int64  `json:"amount_cents"`
  Currency    string `json:"currency"`
}

// Never
type Price struct {
  Amount float64 `json:"amount"`  // never
}
```

## Time

```go
// Store: time.Time (maps to TIMESTAMPTZ in Postgres)
// Display: formatted in branch timezone using branch.Timezone field
loc, _ := time.LoadLocation(branch.Timezone)
displayTime := order.ScheduledAt.In(loc).Format("Mon 2 Jan, 3:04PM")
```

## Testing

```go
// Table-driven unit tests
func TestSlotAvailability(t *testing.T) {
  tests := []struct{
    name string
    input SlotRequest
    want []Slot
    wantErr string
  }{
    {"no capacity", SlotRequest{...}, nil, "SLOT_CAPACITY_EXCEEDED"},
    {"available", SlotRequest{...}, []Slot{{...}}, ""},
  }
  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      // ...
    })
  }
}

// Integration tests — tagged
//go:build integration
// Read DATABASE_GLOBAL_URL, DATABASE_US_URL from env
```

## API documentation (mandatory)

Every shipped endpoint requires:
1. Unit test for handler
2. Swagger/OpenAPI entry in `services/<svc>/api/openapi.yaml`
3. Postman request in `postman/<svc>.postman_collection.json`

Not optional. Not a post-launch task.

## Commit messages

```
feat(orders): add slot capacity reservation to order creation
fix(catalog): correct availability cascade timezone evaluation
docs(phases): update phase 4 with scheduling implementation detail
test(payments): add Stripe webhook idempotency test
```

## What NOT to add without checking ADRs first

- Redis (not needed until scaling beyond one Railway instance)
- Terraform/AWS/GCP (Railway is the target)
- A `services/scheduling/` if it belongs inside `orders/internal/scheduling/`
- Any Stripe webhook processing logic outside `services/payments/`
