-- name: CreatePasswordResetToken :exec
-- Deletes the user's unused tokens first, so only the newest link works. Two requests
-- at the same moment can each leave a token; both still expire within the hour.
WITH deleted AS (
  DELETE FROM password_reset_tokens
  WHERE user_id = sqlc.arg(user_id) AND used_at IS NULL
)
INSERT INTO password_reset_tokens (
  token_hash, user_id, expires_at
) VALUES (
  sqlc.arg(token_hash), sqlc.arg(user_id), now() + interval '1 hour'
);

-- name: PasswordResetTokenUsable :one
SELECT EXISTS (
  SELECT 1 FROM password_reset_tokens
  WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
);

-- name: UsePasswordResetToken :one
-- Of two concurrent uses, the second waits for the first's row lock, then finds used_at
-- set and returns pgx.ErrNoRows.
UPDATE password_reset_tokens SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
RETURNING user_id;
