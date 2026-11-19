-- name: CreateAccount :one
INSERT INTO accounts (name) VALUES ($1)
RETURNING *;

-- name: GetAccountByID :one
SELECT * FROM accounts
WHERE id = $1;

-- name: DeductAccountBalance :one
UPDATE accounts
SET balance = balance - @amount
WHERE id = @id
RETURNING *;

-- name: AddAccountBalance :one
UPDATE accounts
SET balance = balance + @amount
WHERE id = @id
RETURNING *;
