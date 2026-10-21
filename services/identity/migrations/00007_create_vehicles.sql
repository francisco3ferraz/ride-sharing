-- +goose Up
-- One row per driver, so a driver's vehicle details are complete when their row exists.
CREATE TABLE vehicles (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    make text NOT NULL,
    model text NOT NULL,
    colour text NOT NULL,
    -- Only the canonical form: another spelling of a plate would get round UNIQUE.
    plate text NOT NULL UNIQUE CHECK (plate ~ '^[A-Z0-9]{2}-[A-Z0-9]{2}-[A-Z0-9]{2}$')
);

-- +goose Down
DROP TABLE vehicles;
