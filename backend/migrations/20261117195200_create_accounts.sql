-- +goose Up
CREATE TABLE accounts (
  id      BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name    TEXT    NOT NULL,
  balance BIGINT  NOT NULL DEFAULT 0,
  CONSTRAINT balance_nonnegative CHECK (balance >= 0)
);

-- +goose Down
DROP TABLE accounts;