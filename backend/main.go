package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A pool of connections; every request will borrow one and give it back.
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("parse DATABASE_URL: %v", err)
	}
	defer pool.Close()

	// pgxpool.New connects lazily, so Ping is what actually proves the DB is reachable.
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("connect to Postgres: %v", err)
	}

	var version string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		log.Fatalf("query version: %v", err)
	}
	log.Printf("connected: %s", version)
}
