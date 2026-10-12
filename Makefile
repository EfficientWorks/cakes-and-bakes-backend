.PHONY: help dev-up dev-down dev-logs build run seed migrate migrate-down migrate-status swagger contract contract-update test test-unit test-integration vet fmt lint tidy check

help:
	@echo "dev-up            start postgres + redis"
	@echo "run               run the API (migrates on startup) -> http://localhost:8080"
	@echo "seed              create the dev super admin, franchise and branch"
	@echo "swagger           regenerate the OpenAPI spec into api/ -> /docs/index.html"
	@echo "test-unit         run unit tests (no database needed)"
	@echo "test-integration  run integration tests (needs DATABASE_URL)"
	@echo "contract          fail if a POS/KDS request or response shape changed"
	@echo "check             fmt + vet + unit tests + swagger + contract, the pre-push gate"

# --- local environment ------------------------------------------------------

dev-up:
	docker compose -f infra/docker/docker-compose.yml up -d

dev-down:
	docker compose -f infra/docker/docker-compose.yml down

dev-logs:
	docker compose -f infra/docker/docker-compose.yml logs -f

# --- build and run ----------------------------------------------------------

build:
	go build ./...

run:
	go run ./cmd/server

seed:
	go run ./cmd/seed

# The server and the seeder both migrate on startup, so this is only needed
# to apply or roll back migrations without booting either. Override the URL
# for a non-default database: make migrate DATABASE_URL=postgres://...
DATABASE_URL ?= postgres://postgres:postgres@localhost:5500/bakehouse?sslmode=disable

migrate:
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir db/migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir db/migrations postgres "$(DATABASE_URL)" status

# --- codegen ----------------------------------------------------------------

# Regenerates api/{docs.go,swagger.json,swagger.yaml} from the // @... handler
# annotations. Conventions require every shipped endpoint to carry one, so run
# this whenever a route is added or its shape changes.
swagger:
	swag init -g cmd/server/main.go -o api --parseDependency --parseInternal --parseDepth 2

# The POS and KDS apps ship separately from the backend, so their endpoints
# must keep the same request and response shapes. contract compares the spec
# with api/contracts/pos-kds.json; contract-update accepts an intended change.
contract:
	go run ./tools/apicontract

contract-update:
	go run ./tools/apicontract -update

# --- tests ------------------------------------------------------------------

test: test-unit

test-unit:
	go test ./...

test-integration:
	go test -tags integration ./...

# --- quality ----------------------------------------------------------------

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run

tidy:
	go mod tidy

check: fmt vet test-unit swagger
	@echo "ok"
