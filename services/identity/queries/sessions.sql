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
-- such live session, nothing is inserted and the query returns pgx.ErrNoRows. That
-- alone doesn't stop a concurrent revocation, which this statement wouldn't see, so
-- rotation locks the session first.
INSERT INTO refresh_tokens (
  token_hash, session_id, expires_at
)
SELECT
  sqlc.arg(token_hash)::bytea,
  id,
  LEAST(now() + interval '7 days', created_at + interval '30 days')
FROM sessions
WHERE id = sqlc.arg(session_id) AND revoked_at IS NULL
RETURNING expires_at;

-- name: GetRefreshTokenForUpdate :one
-- Run it in a transaction: the locks make rotations and revocations of the session wait
-- for each other, and outside one they end with the statement. The token's row is
-- locked too because a statement that waited for a lock re-reads only the rows it
-- locked, so the second of two concurrent refreshes would see the token unused. The
-- user's row isn't, or all of their sessions would wait for each other. NO KEY UPDATE
-- is the lock the UPDATEs that follow take anyway.
SELECT
  t.session_id,
  s.user_id,
  -- So a role change applies at the next refresh.
  u.role,
  -- The casts are for sqlc, which types IS NOT NULL as interface{}.
  (t.used_at IS NOT NULL)::bool AS used,
  -- By Postgres's clock, which also set expires_at.
  t.expires_at <= now() AS expired,
  (s.revoked_at IS NOT NULL)::bool AS revoked
FROM refresh_tokens t
JOIN sessions s ON s.id = t.session_id
JOIN users u ON u.id = s.user_id
WHERE t.token_hash = $1
FOR NO KEY UPDATE OF t, s;

-- name: MarkRefreshTokenUsed :exec
UPDATE refresh_tokens SET used_at = now()
WHERE token_hash = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionByRefreshToken :exec
-- The UPDATE waits for a rotation in progress, so the token it issues is revoked too.
UPDATE sessions SET revoked_at = now()
WHERE id = (SELECT session_id FROM refresh_tokens WHERE token_hash = $1)
  AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
-- Updates each session row, so it waits for any rotation in progress, as RevokeSession
-- does.
UPDATE sessions SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;
