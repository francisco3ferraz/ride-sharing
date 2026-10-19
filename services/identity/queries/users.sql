-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: CreateUser :one
-- A taken email fails on the users_email_key constraint with code 23505. There's no
-- check first: two registrations with the same email could both pass it.
INSERT INTO users (
  email, password_hash, role
) VALUES (
  $1, $2, $3
)
RETURNING id;

-- name: GetUserByID :one
SELECT id, name, email, role FROM users
WHERE id = $1;

-- name: UpdateUserName :one
UPDATE users SET name = $2
WHERE id = $1
RETURNING id, name, email, role;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2
WHERE id = $1;
