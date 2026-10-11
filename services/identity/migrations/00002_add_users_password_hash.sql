-- +goose Up
-- A NOT NULL column with no default can only be added because users is still empty;
-- Postgres refuses it once any row exists. It's also safe for the previous release,
-- which never inserts users.
ALTER TABLE users ADD COLUMN password_hash text NOT NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN password_hash;
