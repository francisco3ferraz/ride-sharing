package account

import (
	"crypto/ed25519"
	"os"
	"slices"
	"sync"
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

	t.Run("get a registered user", func(t *testing.T) {
		registered, _, err := svc.Register(ctx, "gabriel@example.pt", testPassword, RoleDriver)
		require.NoError(t, err)

		got, err := svc.GetUser(ctx, registered.ID)
		require.NoError(t, err)
		assert.Equal(t, registered, got)
	})

	t.Run("get an unknown user", func(t *testing.T) {
		_, err := svc.GetUser(ctx, uuid.New())
		assert.ErrorIs(t, err, ErrUserNotFound)
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

	t.Run("a refresh token needs a session that isn't revoked", func(t *testing.T) {
		user, _, err := svc.Register(ctx, "helena@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		var sessionID uuid.UUID
		err = pool.QueryRow(ctx, `
			INSERT INTO sessions (user_id, revoked_at) VALUES ($1, now())
			RETURNING id`, user.ID).Scan(&sessionID)
		require.NoError(t, err)

		_, hash := refreshtoken.Generate()
		_, err = db.New(pool).CreateRefreshToken(ctx, db.CreateRefreshTokenParams{TokenHash: hash[:], SessionID: sessionID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("refresh replaces the token within its session", func(t *testing.T) {
		user, first, err := svc.Register(ctx, "ines@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		second, err := svc.Refresh(ctx, first.Refresh.Value)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, second)
		assert.NotEqual(t, first.Refresh.Value, second.Refresh.Value)

		firstSession, firstUsed := refreshTokenRow(t, pool, first.Refresh.Value)
		secondSession, secondUsed := refreshTokenRow(t, pool, second.Refresh.Value)
		assert.True(t, firstUsed)
		assert.False(t, secondUsed)
		assert.Equal(t, firstSession, secondSession)

		third, err := svc.Refresh(ctx, second.Refresh.Value)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, third)
		assert.Equal(t, 1, countSessions(t, pool, user.ID))
	})

	t.Run("refresh signs the user's current role", func(t *testing.T) {
		user, tokens, err := svc.Register(ctx, "joao@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, "UPDATE users SET role = 'driver' WHERE id = $1", user.ID)
		require.NoError(t, err)

		refreshed, err := svc.Refresh(ctx, tokens.Refresh.Value)
		require.NoError(t, err)
		user.Role = RoleDriver
		assertTokens(t, pool, pub, user, refreshed)
	})

	t.Run("reusing a token revokes its session, so the newest token fails too", func(t *testing.T) {
		_, first, err := svc.Register(ctx, "laura@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		second, err := svc.Refresh(ctx, first.Refresh.Value)
		require.NoError(t, err)

		_, err = svc.Refresh(ctx, first.Refresh.Value)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)

		// Fails only if the revocation committed, rather than rolling back with the error.
		_, err = svc.Refresh(ctx, second.Refresh.Value)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		sessionID, _ := refreshTokenRow(t, pool, first.Refresh.Value)
		assert.True(t, sessionRevoked(t, pool, sessionID))
	})

	t.Run("of two concurrent refreshes with one token, one wins and the other ends the session", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "miguel@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		// A missing lock lets both through only some of the time.
		for range 20 {
			_, tokens, err := svc.Login(ctx, "miguel@example.pt", testPassword)
			require.NoError(t, err)

			var results [2]Tokens
			var errs [2]error
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Go(func() { results[i], errs[i] = svc.Refresh(ctx, tokens.Refresh.Value) })
			}
			wg.Wait()

			winner := slices.IndexFunc(errs[:], func(err error) bool { return err == nil })
			require.NotEqual(t, -1, winner, "neither refresh succeeded: %v", errs)
			require.ErrorIs(t, errs[1-winner], ErrInvalidRefreshToken)
			_, err = svc.Refresh(ctx, results[winner].Refresh.Value)
			require.ErrorIs(t, err, ErrInvalidRefreshToken)
		}
	})

	t.Run("refresh rejects unknown, empty and expired tokens, leaving the session alone", func(t *testing.T) {
		_, tokens, err := svc.Register(ctx, "nuno@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		sessionID, _ := refreshTokenRow(t, pool, tokens.Refresh.Value)
		expired, expiredHash := refreshtoken.Generate()
		_, err = pool.Exec(ctx, `
			INSERT INTO refresh_tokens (token_hash, session_id, expires_at)
			VALUES ($1, $2, now() - interval '1 second')`, expiredHash[:], sessionID)
		require.NoError(t, err)

		for _, token := range []string{"not a token", "", expired} {
			_, err := svc.Refresh(ctx, token)
			assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		}

		_, used := refreshTokenRow(t, pool, expired)
		assert.False(t, used)
		_, err = svc.Refresh(ctx, tokens.Refresh.Value)
		assert.NoError(t, err)
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
	var createdAt, expiresAt time.Time
	err = pool.QueryRow(t.Context(), `
		SELECT s.user_id, t.created_at, t.expires_at
		FROM refresh_tokens t JOIN sessions s ON s.id = t.session_id
		WHERE t.token_hash = $1`, hash[:]).Scan(&userID, &createdAt, &expiresAt)
	require.NoError(t, err, "no refresh token stored under the token's hash")
	assert.Equal(t, user.ID, userID)
	assert.WithinDuration(t, expiresAt, tokens.Refresh.ExpiresAt, 0)
	// Both from the same now(), the transaction's start.
	assert.WithinDuration(t, createdAt.Add(7*24*time.Hour), expiresAt, 0)
}

// refreshTokenRow returns the session a stored refresh token belongs to, and whether
// it was used.
func refreshTokenRow(t *testing.T, pool *pgxpool.Pool, token string) (sessionID uuid.UUID, used bool) {
	t.Helper()
	hash := refreshtoken.Hash(token)
	err := pool.QueryRow(t.Context(), "SELECT session_id, used_at IS NOT NULL FROM refresh_tokens WHERE token_hash = $1",
		hash[:]).Scan(&sessionID, &used)
	require.NoError(t, err, "no refresh token stored under the token's hash")
	return sessionID, used
}

func sessionRevoked(t *testing.T, pool *pgxpool.Pool, sessionID uuid.UUID) bool {
	t.Helper()
	var revoked bool
	err := pool.QueryRow(t.Context(), "SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1", sessionID).Scan(&revoked)
	require.NoError(t, err)
	return revoked
}

func countSessions(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions WHERE user_id = $1", userID).Scan(&n)
	require.NoError(t, err)
	return n
}
