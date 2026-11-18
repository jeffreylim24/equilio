# Equilio

A small Stripe-style **payments ledger and webhook service**, written in Go on PostgreSQL.

Merchants hold accounts, move money between them through an API, and are told about each transfer
through signed webhooks. It has few features on purpose. The goal is that every one of them stays
correct when requests race each other, processes crash, and clients retry, and that tests prove it.

> **Status: early work in progress.** Accounts can be created and read. Transfers, the ledger,
> idempotency and webhooks are not built yet. See [Roadmap](#roadmap).

## What it will guarantee

| Guarantee | How |
| --- | --- |
| Money is never created or lost | Double-entry, append-only ledger in integer cents. Every transfer sums to zero. |
| Concurrent transfers don't corrupt balances | Row locks taken in ascending account-ID order, so there are no lost updates or deadlocks. |
| Retried requests don't move money twice | Idempotency keys per merchant, with the stored response replayed. |
| No webhook is lost, even after a crash | Transactional outbox, plus a Postgres-backed job queue (`FOR UPDATE SKIP LOCKED`). |
| Receivers can trust webhooks | HMAC-SHA256 signatures over `timestamp.payload`, with retries, backoff and a dead-letter queue. |

Each guarantee gets a failure-mode test on real Postgres that fails if the fix is removed. Examples:
50 identical requests at once must create exactly one transfer, and `kill -9` on the relay must not
lose any event.

## Stack

- **Go**, standard library `net/http` routing
- **PostgreSQL 17** via [pgx](https://github.com/jackc/pgx), with
  [sqlc](https://sqlc.dev) generating type-safe Go from hand-written SQL (no ORM)
- [goose](https://github.com/pressly/goose) for migrations
- Postgres as the job queue (no message broker)
- Planned: k6 load tests, a small React + TypeScript dashboard, Fly.io deploy, GitHub Actions CI

## Running locally

Requirements: Go, Docker, and the goose CLI:

```sh
go install github.com/pressly/goose/v3/cmd/goose@v3.30.1
```

```sh
# 1. Start Postgres
docker compose up -d

# 2. Configure the backend
cd backend
cp .env.example .env

# 3. Apply migrations (goose reads GOOSE_* from the environment)
set -a; source .env; set +a
goose up

# 4. Run the server (default port 8080)
go run .
```

## API

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| `POST` | `/accounts` | `{"name": "..."}` | `201` with the account; `400` if the name is blank |
| `GET` | `/accounts/{id}` | | `200` with the account; `400` for a bad ID; `404` if not found |

```sh
curl -s -X POST localhost:8080/accounts -d '{"name": "Alice"}'
# {"id":1,"name":"Alice","balance":0}

curl -s localhost:8080/accounts/1
```

Balances are integer cents (SGD) and always start at 0. The API never creates money directly.

## Project layout

```
docker-compose.yml     Postgres 17 for local development
backend/
  main.go              config, DB pool, routes, server
  handlers.go          HTTP handlers
  migrations/          goose SQL migrations (schema)
  queries/             hand-written SQL that sqlc compiles
  db/                  sqlc-generated code (do not edit; run `sqlc generate`)
  sqlc.yaml
```

## Roadmap

- [x] Accounts: schema, queries, create/get endpoints
- [ ] Transfers and the double-entry ledger, with row locking
- [ ] Idempotency keys
- [ ] Transactional outbox, relay and webhook delivery worker
- [ ] Fake merchant receiver with failure knobs
- [ ] Reconciler that checks every ledger invariant
- [ ] Failure-mode tests, plus a chaos run with measured results
- [ ] Merchant API keys, dashboard, CI, deploy
