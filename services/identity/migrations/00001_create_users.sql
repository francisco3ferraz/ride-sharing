-- +goose Up
-- citext compares case-insensitively, so Ana@example.pt and ana@example.pt are one
-- email. It's a trusted extension: the database's owner may install it.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    email citext NOT NULL UNIQUE,
    role text NOT NULL CHECK (role IN ('rider', 'driver')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;
DROP EXTENSION citext;
