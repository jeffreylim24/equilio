-- +goose Up
CREATE TABLE transfers (
  id                BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  from_account_id  BIGINT  NOT NULL REFERENCES accounts(id),
  to_account_id     BIGINT  NOT NULL REFERENCES accounts(id),
  amount            BIGINT  NOT NULL, 
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT amount_positive      CHECK (amount > 0),
  CONSTRAINT accounts_different   CHECK (from_account_id <> to_account_id)
);

-- +goose Down
DROP TABLE transfers;