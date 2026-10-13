-- name: CreateSession :one
INSERT INTO sessions (
  user_id
) VALUES (
  $1
)
RETURNING id;

-- name: CreateRefreshToken :one
-- Expires 7 days from now, but never past 30 days from the session's start.
-- The start is read from the session row, so rotation can reuse this query. With no
-- such session, nothing is inserted and the query returns pgx.ErrNoRows.
INSERT INTO refresh_tokens (
  token_hash, session_id, expires_at
)
SELECT
  sqlc.arg(token_hash)::bytea,
  id,
  LEAST(now() + interval '7 days', created_at + interval '30 days')
FROM sessions
WHERE id = sqlc.arg(session_id)
RETURNING expires_at;
