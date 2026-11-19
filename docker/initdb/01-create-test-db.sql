-- Runs once, only when the Postgres data volume is empty (first `docker compose up`).
-- Creates the database integration tests use, next to the `equilio` dev database.
CREATE DATABASE equilio_test;
