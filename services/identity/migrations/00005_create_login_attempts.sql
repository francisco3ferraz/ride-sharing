-- +goose Up
-- Login attempts per (email, client address) pair, for the lockout. An attempt counts
-- before its password is checked, and a successful login deletes its pair's row, so
-- the attempts counted have all failed or are still in flight. The email is as typed,
-- registered or not, so there's no foreign key to users. citext, as in users, makes
-- every case of one email a single pair.
CREATE TABLE login_attempts (
    email citext NOT NULL,
    ip inet NOT NULL,
    attempts integer NOT NULL DEFAULT 1 CHECK (attempts >= 0),
    window_started_at timestamptz NOT NULL DEFAULT now(),
    -- Set by the failure that reaches the limit.
    blocked_until timestamptz,
    PRIMARY KEY (email, ip)
);

-- +goose Down
DROP TABLE login_attempts;
