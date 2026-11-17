-- name: CreateAccount :one
INSERT INTO accounts (name) VALUES ($1)
RETURNING *;

-- name: GetAccountByID :one
SELECT * FROM accounts
WHERE id = $1;