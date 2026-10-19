-- +goose Up
-- Nullable, unlike 00002's column: users has rows now, and during a rolling update the
-- previous release still inserts users without a name. NULL means never set. The
-- previous release's SELECT * queries still work, because sqlc expanded them into
-- column lists when it generated them.
ALTER TABLE users ADD COLUMN name text;

-- +goose Down
ALTER TABLE users DROP COLUMN name;
