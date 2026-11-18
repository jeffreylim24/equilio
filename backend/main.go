// Command backend runs the Equilio payments API: merchants create accounts and
// move money between them over HTTP.
//
// It reads DATABASE_URL (required) and PORT (default 8080) from the environment.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeffreylim24/equilio/backend/db"
)

const defaultPort = "8080"

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Bounds startup only. Handlers must use r.Context(): this one expires after 5 s.
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

	srv := &Server{queries: db.New(pool)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /accounts", srv.handleCreateAccount)
	mux.HandleFunc("GET /accounts/{id}", srv.handleGetAccountByID)
	log.Printf("listening on %v", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
