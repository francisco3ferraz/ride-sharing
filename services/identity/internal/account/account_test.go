package account

import (
	"crypto/ed25519"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/accesstoken"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/db"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/refreshtoken"
)

const testPassword = "a long enough password"

func TestService(t *testing.T) {
	ctx := t.Context()

	// The same Postgres build as the cluster's CNPG image, which testcontainers can't
	// start since it has no entrypoint. Bump both together.
	ctr, err := postgres.Run(ctx, "postgres:18.6-trixie", postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	connString, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	migrations, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), os.DirFS("../../migrations"))
	require.NoError(t, err)
	_, err = migrations.Up(ctx)
	require.NoError(t, err)

	pub, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	svc := New(pool, password.New(), accesstoken.New(key, time.Now))

	t.Run("register, then log in with the email in another case", func(t *testing.T) {
		registered, _, err := svc.Register(ctx, "ana@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, registered.ID)
		assert.Equal(t, User{ID: registered.ID, Email: "ana@example.pt", Role: RoleRider}, registered)

		loggedIn, _, err := svc.Login(ctx, "Ana@Example.pt", testPassword)
		require.NoError(t, err)
		assert.Equal(t, registered, loggedIn)
	})

	t.Run("register and each login start a session", func(t *testing.T) {
		user, tokens, err := svc.Register(ctx, "eva@example.pt", testPassword, RoleDriver)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, tokens)

		_, first, err := svc.Login(ctx, "eva@example.pt", testPassword)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, first)

		_, second, err := svc.Login(ctx, "eva@example.pt", testPassword)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, second)

		assert.NotEqual(t, first.Refresh.Value, second.Refresh.Value)
		assert.Equal(t, 3, countSessions(t, pool, user.ID))
	})

	t.Run("taken email, in another case", func(t *testing.T) {
		first, _, err := svc.Register(ctx, "bruno@example.pt", testPassword, RoleDriver)
		require.NoError(t, err)

		_, _, err = svc.Register(ctx, "Bruno@example.pt", testPassword, RoleRider)
		assert.ErrorIs(t, err, ErrEmailTaken)
		assert.Equal(t, 1, countSessions(t, pool, first.ID))
	})

	t.Run("password breaks the rule", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "carla@example.pt", "too short", RoleRider)
		require.ErrorIs(t, err, password.ErrInvalid)
		assert.Equal(t, "invalid password: must be at least 15 characters", err.Error())
	})

	t.Run("wrong password and unknown email fail the same way", func(t *testing.T) {
		user, _, err := svc.Register(ctx, "diana@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		_, _, err = svc.Login(ctx, "diana@example.pt", "not the right password")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		_, _, err = svc.Login(ctx, "nobody@example.pt", testPassword)
		assert.ErrorIs(t, err, ErrInvalidCredentials)

		assert.Equal(t, 1, countSessions(t, pool, user.ID))
	})

	// Rotation reuses the refresh token query on old sessions. One that Register or Login
	// has just created never reaches the cap.
	t.Run("a refresh token never outlives its session's 30 days", func(t *testing.T) {
		user, _, err := svc.Register(ctx, "filipa@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		var sessionID uuid.UUID
		var sessionStart time.Time
		err = pool.QueryRow(ctx, `
			INSERT INTO sessions (user_id, created_at) VALUES ($1, now() - interval '25 days')
			RETURNING id, created_at`, user.ID).Scan(&sessionID, &sessionStart)
		require.NoError(t, err)

		_, hash := refreshtoken.Generate()
		expiresAt, err := db.New(pool).CreateRefreshToken(ctx, db.CreateRefreshTokenParams{TokenHash: hash[:], SessionID: sessionID})
		require.NoError(t, err)
		assert.WithinDuration(t, sessionStart.Add(30*24*time.Hour), expiresAt, 0)
	})

	t.Run("a refresh token needs an existing session", func(t *testing.T) {
		_, hash := refreshtoken.Generate()
		_, err := db.New(pool).CreateRefreshToken(ctx, db.CreateRefreshTokenParams{TokenHash: hash[:], SessionID: uuid.New()})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})
}

// assertTokens checks that the access token verifies as the Edge will verify it, and that
// the database holds the refresh token's hash, for a session of the user's.
func assertTokens(t *testing.T, pool *pgxpool.Pool, pub ed25519.PublicKey, user User, tokens Tokens) {
	t.Helper()

	var claims accesstoken.Claims
	_, err := jwt.ParseWithClaims(tokens.Access.Value, &claims,
		func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(accesstoken.Issuer),
		jwt.WithAudience(accesstoken.Audience),
	)
	require.NoError(t, err)
	assert.Equal(t, user.ID.String(), claims.Subject)
	assert.Equal(t, string(user.Role), claims.Role)
	assert.WithinDuration(t, claims.ExpiresAt.Time, tokens.Access.ExpiresAt, 0)

	hash := refreshtoken.Hash(tokens.Refresh.Value)
	var userID uuid.UUID
	var sessionStart, expiresAt time.Time
	err = pool.QueryRow(t.Context(), `
		SELECT s.user_id, s.created_at, t.expires_at
		FROM refresh_tokens t JOIN sessions s ON s.id = t.session_id
		WHERE t.token_hash = $1`, hash[:]).Scan(&userID, &sessionStart, &expiresAt)
	require.NoError(t, err, "no refresh token stored under the token's hash")
	assert.Equal(t, user.ID, userID)
	assert.WithinDuration(t, expiresAt, tokens.Refresh.ExpiresAt, 0)
	// Created in the session's transaction, so with the same now() as the session.
	assert.WithinDuration(t, sessionStart.Add(7*24*time.Hour), expiresAt, 0)
}

func countSessions(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions WHERE user_id = $1", userID).Scan(&n)
	require.NoError(t, err)
	return n
}
