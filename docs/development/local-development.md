# Local development

## Prerequisites

- Go 1.22+
- Docker + Docker Compose
- `buf` CLI (for protobuf generation)
- `goose` CLI (for migrations)
- `sqlc` CLI (for query generation)

## Start the local environment

```bash
# Start Postgres (creates bb_global + bb_us databases)
docker compose -f infra/docker/docker-compose.yml up -d

# Copy env file
cp .env.example .env
# Edit .env: fill in STRIPE_SECRET_KEY, POSTMARK_API_KEY, GOOGLE_MAPS_API_KEY

# Run all migrations
make migrate

# Verify
make test-unit
```

## Environment variables

```env
# Databases
DATABASE_GLOBAL_URL=postgres://bb:bb@localhost:5432/bb_global?sslmode=disable
DATABASE_US_URL=postgres://bb:bb@localhost:5432/bb_us?sslmode=disable

# Auth
JWT_PRIVATE_KEY=<EdDSA private key — generate with make gen-keys>
JWT_PUBLIC_KEY=<EdDSA public key>

# External services
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
POSTMARK_API_TOKEN=...
GOOGLE_MAPS_API_KEY=...

# App
PORT=8080
ENV=development
LOG_LEVEL=debug
```

## Migration run order (fresh environment)

Services share the global database. Order matters:

```bash
# 1. Franchise global (creates registry tables identity reads)
goose -dir services/franchise/db/migrations/global postgres $DATABASE_GLOBAL_URL up

# 2. Identity global
goose -dir services/identity/db/migrations/global postgres $DATABASE_GLOBAL_URL up

# 3. Regional (any order)
goose -dir services/franchise/db/migrations/regional postgres $DATABASE_US_URL up
goose -dir services/identity/db/migrations/regional postgres $DATABASE_US_URL up
goose -dir services/catalog/db/migrations postgres $DATABASE_US_URL up
goose -dir services/orders/db/migrations postgres $DATABASE_US_URL up
goose -dir services/payments/db/migrations postgres $DATABASE_US_URL up
goose -dir services/admin/db/migrations postgres $DATABASE_US_URL up

# Or use the Makefile shortcut:
make migrate
```

## Running a service

```bash
# Run the identity service
go run ./services/identity/cmd/server/

# Run all services (one terminal per service, or use make run-all)
make run-all
```

## Running tests

```bash
# Unit tests only
make test-unit

# Integration tests (requires running Postgres)
make test-integration

# Specific service
go test ./services/orders/... -tags integration
```

## Generating code

```bash
# Regenerate protobuf / ConnectRPC
make proto

# Regenerate sqlc queries
make sqlc

# Both
make generate
```

## Seeded dev data

After `make migrate`, the following exist:

- Super admin: `admin@bakedbouquet.com` / `devpassword123` (realm: support)
- Test branch: `BB-NJ-001` (Edison NJ) in `bb_us`
- Staff PIN for terminal login: `1234` (counter_staff role, branch BB-NJ-001)

## Stripe local testing

Install Stripe CLI for webhook forwarding:

```bash
stripe listen --forward-to localhost:8080/v1/webhooks/stripe
```

This populates `STRIPE_WEBHOOK_SECRET` in the terminal output — copy to `.env`.
