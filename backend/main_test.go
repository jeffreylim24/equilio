package main

// Test harness: TestMain runs once before every test in this package. It connects to the
// test database (D15), runs the goose migrations into it, and shares the pool via testPool.
//
// Run from backend/: set -a; source .env; set +a; go test ./...

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

// testPool is the connection pool every test shares. Set by TestMain before any test runs.
var testPool *pgxpool.Pool

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

// setupTestDB connects to TEST_DATABASE_URL, refuses any database not named *_test,
// and brings its schema up to date with migrations/.
func setupTestDB(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		return nil, errors.New("TEST_DATABASE_URL is not set (see .env.example)")
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	// Guard against pointing tests at the dev DB: tests delete data.
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

	// go test runs in the package directory (backend/), so this relative path works.
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

// resetDB empties every app table so the calling test starts from a known state.
// Call it first in every test. It runs at the start, not the end, so a failed test's
// rows stay in equilio_test for debugging in psql until the next test runs.
//
// goose_db_version is deliberately not listed: wiping it would make goose re-run
// migrations into tables that already exist. No CASCADE: when a new table references
// accounts or transfers and isn't added here, TRUNCATE fails loudly instead of
// silently emptying it.
func resetDB(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(t.Context(), "TRUNCATE transfers, accounts RESTART IDENTITY")
	if err != nil {
		t.Fatalf("reset test DB: %v", err)
	}
}
