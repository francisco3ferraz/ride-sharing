-- +goose Up
-- Nullable: users has rows, and during a rolling update the previous release still
-- inserts users without a name.
ALTER TABLE users ADD COLUMN name text;

-- +goose Down
ALTER TABLE users DROP COLUMN name;
