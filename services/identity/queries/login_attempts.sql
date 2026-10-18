-- name: CountLoginAttempt :one
-- Counts an attempt for the pair and returns the count in its window, and whether the
-- pair is blocked. A window lasts 15 minutes from its first attempt, or for as long as
-- the pair is blocked; the next attempt after that starts a new one. Concurrent attempts
-- take turns on the row's lock, so each gets a count of its own.
INSERT INTO login_attempts AS a (
  email, ip
) VALUES (
  sqlc.arg(email), sqlc.arg(ip)
)
ON CONFLICT (email, ip) DO UPDATE SET
  attempts = CASE
    WHEN a.window_started_at > now() - interval '15 minutes' OR a.blocked_until > now()
    THEN a.attempts + 1
    ELSE 1
  END,
  window_started_at = CASE
    WHEN a.window_started_at > now() - interval '15 minutes' OR a.blocked_until > now()
    THEN a.window_started_at
    ELSE now()
  END,
  blocked_until = CASE WHEN a.blocked_until > now() THEN a.blocked_until END
-- The cast is for sqlc, which otherwise types blocked as interface{}.
RETURNING attempts, (blocked_until IS NOT NULL)::boolean AS blocked;

-- name: BlockLoginAttempts :exec
-- Blocks the pair for 15 minutes once its count reaches max_attempts. A pair that's
-- already blocked keeps its end time.
UPDATE login_attempts SET blocked_until = now() + interval '15 minutes'
WHERE email = sqlc.arg(email) AND ip = sqlc.arg(ip)
  AND attempts >= sqlc.arg(max_attempts)::int AND blocked_until IS NULL;

-- name: ReleaseLoginAttempt :exec
-- Stops counting an attempt that ended without its password being checked.
UPDATE login_attempts SET attempts = attempts - 1
WHERE email = sqlc.arg(email) AND ip = sqlc.arg(ip) AND attempts > 0;

-- name: ClearLoginAttempts :exec
DELETE FROM login_attempts
WHERE email = sqlc.arg(email) AND ip = sqlc.arg(ip);

-- name: ClearUserLoginAttempts :exec
-- Every pair for the user's email, from any address.
DELETE FROM login_attempts
WHERE email = (SELECT email FROM users WHERE id = sqlc.arg(user_id));
