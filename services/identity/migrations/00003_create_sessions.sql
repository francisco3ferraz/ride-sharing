-- +goose Up
CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE refresh_tokens (
    -- SHA-256 of the token
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- 7 days after issue, but never past the session's 30 days.
    expires_at timestamptz NOT NULL,
    used_at timestamptz
);

CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE sessions;
