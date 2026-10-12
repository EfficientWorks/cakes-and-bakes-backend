// Package db wires the Postgres connection pool and runs migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver used only for goose
)

// NewPool opens a pgx connection pool against databaseURL.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies all pending goose migrations found under migrationsDir
// (e.g. "db/migrations", relative to the process working directory — the
// repo root when run via `make run` or `go run ./cmd/api`). It opens its own
// database/sql connection (goose requires one) and closes it before returning.
func Migrate(databaseURL, migrationsDir string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer sqlDB.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(sqlDB, migrationsDir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
