package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// testPool is shared by every test in the package; TestMain sets it.
var testPool *pgxpool.Pool

// TestMain prepares the test database once, then runs every test in the package.
func TestMain(m *testing.M) {
	// Bounds setup only (connect + migrate), not the tests themselves.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	pool, err := setupTestDB(ctx)
	cancel()
	if err != nil {
		// Fail loudly: skipping would show a green run with nothing tested.
		log.Fatalf("test setup: %v", err)
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	os.Exit(code)
}

// setupTestDB connects to TEST_DATABASE_URL and brings its schema up to date. It refuses
// any database not named *_test, because tests delete data.
func setupTestDB(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		return nil, errors.New("TEST_DATABASE_URL is not set (see .env.example)")
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	if name := cfg.ConnConfig.Database; !strings.HasSuffix(name, "_test") {
		return nil, fmt.Errorf("refusing to run: database %q does not end in _test", name)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to test DB: %w", err)
	}

	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// migrate applies any pending migrations from migrations/ (the same files the goose CLI uses).
// goose needs a *sql.DB, so this wraps the pgx pool in one; closing it leaves the pool open.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	// Relative to backend/: go test runs in the package directory.
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS("migrations"))
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	for _, r := range results {
		log.Printf("migrate: %v", r)
	}
	return nil
}

// resetDB empties the app tables. Call it first in every test: resetting at the start
// rather than the end leaves a failed test's rows in place for debugging.
//
// goose_db_version is not listed, or goose would re-run migrations. No CASCADE, so a new
// table referencing these that is missing from the list makes TRUNCATE fail loudly.
func resetDB(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(t.Context(), "TRUNCATE transfers, accounts RESTART IDENTITY")
	if err != nil {
		t.Fatalf("reset test DB: %v", err)
	}
}
