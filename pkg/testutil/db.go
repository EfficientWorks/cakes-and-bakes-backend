//go:build integration

// Integration-test fixtures. Tagged so `go test ./...` stays fast and needs
// no database; `make test-integration` opts in.
package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/db"
)

// PoolFromEnv opens a pool against DATABASE_URL and skips the test when it
// is unset, so a developer without Postgres running gets a skip rather than
// a failure. Migrations are applied once per pool.
func PoolFromEnv(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	if err := db.Migrate(url, "../../../db/migrations"); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	pool, err := db.NewPool(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Truncate empties the given tables, restarting identity sequences and
// cascading to dependents. Call it at the top of a test rather than at the
// end, so a failed run leaves its rows behind for inspection.
func Truncate(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if _, err := pool.Exec(context.Background(),
			"TRUNCATE TABLE "+table+" RESTART IDENTITY CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}
