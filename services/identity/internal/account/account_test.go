package account

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/netip"
	"os"
	"slices"
	"strings"
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

var (
	testIP  = netip.MustParseAddr("203.0.113.7")
	otherIP = netip.MustParseAddr("2001:db8::1")
)

func TestCleanName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "kept as typed", input: "Ana Sofia", want: "Ana Sofia"},
		{name: "trimmed", input: " \tAna\u00a0", want: "Ana"},
		{name: "empty", input: "", wantErr: "invalid name: must not be empty"},
		{name: "only spaces", input: "   ", wantErr: "invalid name: must not be empty"},
		{name: "50 characters", input: strings.Repeat("a", 50), want: strings.Repeat("a", 50)},
		{name: "51 characters", input: strings.Repeat("a", 51), wantErr: "invalid name: must be at most 50 characters"},
		{name: "counts characters, not bytes", input: strings.Repeat("ã", 50), want: strings.Repeat("ã", 50)},
		{name: "NUL", input: "Ana\x00", wantErr: "invalid name: must not contain control characters"},
		{name: "line break", input: "Ana\nSofia", wantErr: "invalid name: must not contain control characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanName(tt.input)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, ErrInvalidName)
				assert.Equal(t, tt.wantErr, err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

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
	mailer := &fakeMailer{sent: make(chan sentEmail, 10)}
	svc := New(pool, password.New(), accesstoken.New(key, time.Now), mailer)

	t.Run("register, then log in with the email in another case", func(t *testing.T) {
		registered, _, err := svc.Register(ctx, "ana@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, registered.ID)
		assert.Equal(t, User{ID: registered.ID, Email: "ana@example.pt", Role: RoleRider}, registered)

		loggedIn, _, err := svc.Login(ctx, "Ana@Example.pt", testPassword, testIP)
		require.NoError(t, err)
		assert.Equal(t, registered, loggedIn)
	})

	t.Run("register and each login start a session", func(t *testing.T) {
		user, tokens, err := svc.Register(ctx, "eva@example.pt", testPassword, RoleDriver)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, tokens)

		_, first, err := svc.Login(ctx, "eva@example.pt", testPassword, testIP)
		require.NoError(t, err)
		assertTokens(t, pool, pub, user, first)

		_, second, err := svc.Login(ctx, "eva@example.pt", testPassword, testIP)
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

		_, _, err = svc.Login(ctx, "diana@example.pt", "not the right password", testIP)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		_, _, err = svc.Login(ctx, "nobody@example.pt", testPassword, testIP)
		assert.ErrorIs(t, err, ErrInvalidCredentials)

		assert.Equal(t, 1, countSessions(t, pool, user.ID))
	})

	t.Run("five failed logins block the pair, even for the right password", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "yara@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		failLogins(t, svc, "yara@example.pt", testIP, 5)

		_, _, err = svc.Login(ctx, "yara@example.pt", testPassword, testIP)
		assert.ErrorIs(t, err, ErrTooManyLoginAttempts)
		// The same email in another case is the same pair.
		_, _, err = svc.Login(ctx, "Yara@Example.pt", testPassword, testIP)
		assert.ErrorIs(t, err, ErrTooManyLoginAttempts)
		_, _, err = svc.Login(ctx, "yara@example.pt", testPassword, otherIP)
		assert.NoError(t, err, "another address isn't blocked")
	})

	t.Run("an unknown email is blocked the same way", func(t *testing.T) {
		failLogins(t, svc, "nobody-yet@example.pt", testIP, 5)

		_, _, err := svc.Login(ctx, "nobody-yet@example.pt", testPassword, testIP)
		assert.ErrorIs(t, err, ErrTooManyLoginAttempts)
	})

	t.Run("a successful login clears its pair", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "zara@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		failLogins(t, svc, "zara@example.pt", testIP, 4)
		_, _, err = svc.Login(ctx, "zara@example.pt", testPassword, testIP)
		require.NoError(t, err)

		failLogins(t, svc, "zara@example.pt", testIP, 4)
		_, _, err = svc.Login(ctx, "zara@example.pt", testPassword, testIP)
		assert.NoError(t, err)
	})

	t.Run("a block ends after 15 minutes", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "abel@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		failLogins(t, svc, "abel@example.pt", testIP, 5)

		_, err = pool.Exec(ctx, `
			UPDATE login_attempts SET
				window_started_at = window_started_at - interval '15 minutes',
				blocked_until = blocked_until - interval '15 minutes'
			WHERE email = 'abel@example.pt'`)
		require.NoError(t, err)

		_, _, err = svc.Login(ctx, "abel@example.pt", testPassword, testIP)
		assert.NoError(t, err)
	})

	t.Run("failures spread over more than 15 minutes don't block", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "beto@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		failLogins(t, svc, "beto@example.pt", testIP, 4)

		_, err = pool.Exec(ctx, `
			UPDATE login_attempts SET window_started_at = window_started_at - interval '15 minutes'
			WHERE email = 'beto@example.pt'`)
		require.NoError(t, err)

		failLogins(t, svc, "beto@example.pt", testIP, 4)
		_, _, err = svc.Login(ctx, "beto@example.pt", testPassword, testIP)
		assert.NoError(t, err)
	})

	t.Run("of many concurrent wrong passwords, only five are checked", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "celia@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		var errs [20]error
		var wg sync.WaitGroup
		for i := range errs {
			wg.Go(func() { _, _, errs[i] = svc.Login(ctx, "celia@example.pt", "not the right password", testIP) })
		}
		wg.Wait()

		var checked, blocked int
		for _, err := range errs {
			switch {
			case errors.Is(err, ErrInvalidCredentials):
				checked++
			case errors.Is(err, ErrTooManyLoginAttempts):
				blocked++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}
		assert.Equal(t, 5, checked)
		assert.Equal(t, 15, blocked)
	})

	t.Run("a login that ends without checking the password isn't counted", func(t *testing.T) {
		user, _, err := svc.Register(ctx, "dario@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		// Verify fails on a hash it didn't make, as it does when the caller gives up
		// waiting for a hashing slot.
		_, err = pool.Exec(ctx, "UPDATE users SET password_hash = 'not a hash' WHERE id = $1", user.ID)
		require.NoError(t, err)

		for range 6 {
			_, _, err = svc.Login(ctx, "dario@example.pt", testPassword, testIP)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrTooManyLoginAttempts)
			require.NotErrorIs(t, err, ErrInvalidCredentials)
		}
		var attempts int
		err = pool.QueryRow(ctx, "SELECT attempts FROM login_attempts WHERE email = 'dario@example.pt'").Scan(&attempts)
		require.NoError(t, err)
		assert.Zero(t, attempts)
	})

	t.Run("a right password whose session can't start isn't counted", func(t *testing.T) {
		user, _, err := svc.Register(ctx, "edite@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		// Fails the login's transaction after the password checks out, as a context that
		// ends during the hash does. NOT VALID leaves its existing session alone.
		_, err = pool.Exec(ctx, "ALTER TABLE sessions ADD CONSTRAINT no_edite CHECK (user_id <> '"+user.ID.String()+"') NOT VALID")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := pool.Exec(ctx, "ALTER TABLE sessions DROP CONSTRAINT no_edite")
			assert.NoError(t, err)
		})

		for range 6 {
			_, _, err = svc.Login(ctx, "edite@example.pt", testPassword, testIP)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrTooManyLoginAttempts)
			require.NotErrorIs(t, err, ErrInvalidCredentials)
		}
		var attempts int
		err = pool.QueryRow(ctx, "SELECT attempts FROM login_attempts WHERE email = 'edite@example.pt'").Scan(&attempts)
		require.NoError(t, err)
		assert.Zero(t, attempts)
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

	t.Run("set a name, then replace it", func(t *testing.T) {
		registered, _, err := svc.Register(ctx, "lara@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		assert.Empty(t, registered.Name)

		updated, err := svc.UpdateUser(ctx, registered.ID, UserUpdate{Name: new("  Lara  ")})
		require.NoError(t, err)
		assert.Equal(t, User{ID: registered.ID, Name: "Lara", Email: "lara@example.pt", Role: RoleRider}, updated)

		updated, err = svc.UpdateUser(ctx, registered.ID, UserUpdate{Name: new("Lara Sousa")})
		require.NoError(t, err)
		assert.Equal(t, "Lara Sousa", updated.Name)

		got, err := svc.GetUser(ctx, registered.ID)
		require.NoError(t, err)
		assert.Equal(t, updated, got)
		loggedIn, _, err := svc.Login(ctx, "lara@example.pt", testPassword, testIP)
		require.NoError(t, err)
		assert.Equal(t, updated, loggedIn)
	})

	t.Run("an update with nothing to change returns the user as they are", func(t *testing.T) {
		registered, _, err := svc.Register(ctx, "marta@example.pt", testPassword, RoleDriver)
		require.NoError(t, err)
		named, err := svc.UpdateUser(ctx, registered.ID, UserUpdate{Name: new("Marta")})
		require.NoError(t, err)

		got, err := svc.UpdateUser(ctx, registered.ID, UserUpdate{})
		require.NoError(t, err)
		assert.Equal(t, named, got)
	})

	t.Run("a name that breaks the rule leaves the old one", func(t *testing.T) {
		registered, _, err := svc.Register(ctx, "wanda@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		_, err = svc.UpdateUser(ctx, registered.ID, UserUpdate{Name: new("Wanda")})
		require.NoError(t, err)

		_, err = svc.UpdateUser(ctx, registered.ID, UserUpdate{Name: new("Wan\x00da")})
		require.ErrorIs(t, err, ErrInvalidName)

		got, err := svc.GetUser(ctx, registered.ID)
		require.NoError(t, err)
		assert.Equal(t, "Wanda", got.Name)
	})

	t.Run("update an unknown user", func(t *testing.T) {
		_, err := svc.UpdateUser(ctx, uuid.New(), UserUpdate{Name: new("Ana")})
		require.ErrorIs(t, err, ErrUserNotFound)

		_, err = svc.UpdateUser(ctx, uuid.New(), UserUpdate{})
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
			_, tokens, err := svc.Login(ctx, "miguel@example.pt", testPassword, testIP)
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

	t.Run("logout revokes its own session only", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "olga@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		_, phone, err := svc.Login(ctx, "olga@example.pt", testPassword, testIP)
		require.NoError(t, err)
		_, laptop, err := svc.Login(ctx, "olga@example.pt", testPassword, testIP)
		require.NoError(t, err)

		require.NoError(t, svc.Logout(ctx, phone.Refresh.Value))

		_, err = svc.Refresh(ctx, phone.Refresh.Value)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		_, err = svc.Refresh(ctx, laptop.Refresh.Value)
		assert.NoError(t, err)
	})

	t.Run("logout succeeds with tokens it can't refresh", func(t *testing.T) {
		_, tokens, err := svc.Register(ctx, "paulo@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		refreshed, err := svc.Refresh(ctx, tokens.Refresh.Value)
		require.NoError(t, err)

		// A used token, as a stale tab sends, still ends the session.
		require.NoError(t, svc.Logout(ctx, tokens.Refresh.Value))
		_, err = svc.Refresh(ctx, refreshed.Refresh.Value)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)

		for _, token := range []string{tokens.Refresh.Value, "not a token", ""} {
			assert.NoError(t, svc.Logout(ctx, token))
		}
	})

	t.Run("a reset link, asked for in another case, replaces the password once", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "quim@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		require.NoError(t, svc.RequestPasswordReset(ctx, "Quim@Example.pt"))
		email := nextEmail(t, mailer)
		assert.Equal(t, "quim@example.pt", email.to)
		hash := refreshtoken.Hash(email.token)
		var validSeconds int
		err = pool.QueryRow(ctx, `
			SELECT extract(epoch FROM expires_at - created_at)::int
			FROM password_reset_tokens WHERE token_hash = $1`, hash[:]).Scan(&validSeconds)
		require.NoError(t, err, "no reset token stored under the token's hash")
		assert.Equal(t, 3600, validSeconds)

		require.NoError(t, svc.ResetPassword(ctx, email.token, "a brand new long password"))
		_, _, err = svc.Login(ctx, "quim@example.pt", testPassword, testIP)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		_, _, err = svc.Login(ctx, "quim@example.pt", "a brand new long password", testIP)
		assert.NoError(t, err)

		err = svc.ResetPassword(ctx, email.token, "another long enough password")
		assert.ErrorIs(t, err, ErrInvalidResetToken)
	})

	t.Run("a reset revokes every session of the user, and no one else's", func(t *testing.T) {
		_, phone, err := svc.Register(ctx, "rita@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		_, laptop, err := svc.Login(ctx, "rita@example.pt", testPassword, testIP)
		require.NoError(t, err)
		_, other, err := svc.Register(ctx, "sara@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		require.NoError(t, svc.RequestPasswordReset(ctx, "rita@example.pt"))
		require.NoError(t, svc.ResetPassword(ctx, nextEmail(t, mailer).token, "a brand new long password"))

		_, err = svc.Refresh(ctx, phone.Refresh.Value)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		_, err = svc.Refresh(ctx, laptop.Refresh.Value)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		_, err = svc.Refresh(ctx, other.Refresh.Value)
		assert.NoError(t, err)
	})

	t.Run("a reset lifts the email's lockouts from every address, and no one else's", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "elsa@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		failLogins(t, svc, "elsa@example.pt", testIP, 5)
		failLogins(t, svc, "Elsa@Example.pt", otherIP, 5)
		failLogins(t, svc, "fred@example.pt", testIP, 5)

		require.NoError(t, svc.RequestPasswordReset(ctx, "elsa@example.pt"))
		require.NoError(t, svc.ResetPassword(ctx, nextEmail(t, mailer).token, "a brand new long password"))

		_, _, err = svc.Login(ctx, "elsa@example.pt", "a brand new long password", testIP)
		assert.NoError(t, err)
		_, _, err = svc.Login(ctx, "elsa@example.pt", "a brand new long password", otherIP)
		assert.NoError(t, err)
		_, _, err = svc.Login(ctx, "fred@example.pt", testPassword, testIP)
		assert.ErrorIs(t, err, ErrTooManyLoginAttempts)
	})

	t.Run("a reset for an unknown email sends nothing, and doesn't fail", func(t *testing.T) {
		require.NoError(t, svc.RequestPasswordReset(ctx, "nobody@example.pt"))
		// No goroutine started, so there's nothing to wait for.
		assert.Empty(t, mailer.sent)
	})

	t.Run("asking for another reset link makes the older one stop working", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "tiago@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		// Each email is awaited before the next request, since sends can finish in any order.
		require.NoError(t, svc.RequestPasswordReset(ctx, "tiago@example.pt"))
		first := nextEmail(t, mailer)
		require.NoError(t, svc.RequestPasswordReset(ctx, "tiago@example.pt"))
		second := nextEmail(t, mailer)

		err = svc.ResetPassword(ctx, first.token, "a brand new long password")
		assert.ErrorIs(t, err, ErrInvalidResetToken)
		assert.NoError(t, svc.ResetPassword(ctx, second.token, "a brand new long password"))
	})

	t.Run("a reset rejects unknown, empty and expired tokens", func(t *testing.T) {
		user, _, err := svc.Register(ctx, "ursula@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		expired, expiredHash := refreshtoken.Generate()
		_, err = pool.Exec(ctx, `
			INSERT INTO password_reset_tokens (token_hash, user_id, expires_at)
			VALUES ($1, $2, now() - interval '1 second')`, expiredHash[:], user.ID)
		require.NoError(t, err)

		for _, token := range []string{"not a token", "", expired} {
			err := svc.ResetPassword(ctx, token, "a brand new long password")
			assert.ErrorIs(t, err, ErrInvalidResetToken)
		}
		_, _, err = svc.Login(ctx, "ursula@example.pt", testPassword, testIP)
		assert.NoError(t, err)
	})

	t.Run("a new password that breaks the rule leaves the link working", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "vasco@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		require.NoError(t, svc.RequestPasswordReset(ctx, "vasco@example.pt"))
		token := nextEmail(t, mailer).token

		err = svc.ResetPassword(ctx, token, "too short")
		assert.ErrorIs(t, err, password.ErrInvalid)
		assert.NoError(t, svc.ResetPassword(ctx, token, "a brand new long password"))
	})

	t.Run("of two concurrent resets with one link, only one succeeds", func(t *testing.T) {
		_, _, err := svc.Register(ctx, "xavier@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		// Both pass the check before hashing, so only the UPDATE can stop the second.
		for range 3 {
			require.NoError(t, svc.RequestPasswordReset(ctx, "xavier@example.pt"))
			token := nextEmail(t, mailer).token

			var errs [2]error
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Go(func() { errs[i] = svc.ResetPassword(ctx, token, "a brand new long password") })
			}
			wg.Wait()

			winner := slices.IndexFunc(errs[:], func(err error) bool { return err == nil })
			require.NotEqual(t, -1, winner, "neither reset succeeded: %v", errs)
			require.ErrorIs(t, errs[1-winner], ErrInvalidResetToken)
		}
	})
}

// fakeMailer hands each email to the test through sent, since the service sends them in
// the background.
type fakeMailer struct {
	sent chan sentEmail
}

type sentEmail struct {
	to    string
	token string
}

func (f *fakeMailer) SendPasswordReset(_ context.Context, to, token string) error {
	f.sent <- sentEmail{to: to, token: token}
	return nil
}

func nextEmail(t *testing.T, mailer *fakeMailer) sentEmail {
	t.Helper()
	select {
	case email := <-mailer.sent:
		return email
	case <-time.After(5 * time.Second):
		t.Fatal("no email sent")
		return sentEmail{}
	}
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

// failLogins logs in n times with a wrong password, each failing as wrong credentials.
func failLogins(t *testing.T, svc *Service, email string, clientIP netip.Addr, n int) {
	t.Helper()
	for i := range n {
		_, _, err := svc.Login(t.Context(), email, "not the right password", clientIP)
		require.ErrorIs(t, err, ErrInvalidCredentials, "failure %d", i+1)
	}
}
