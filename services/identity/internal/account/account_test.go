package account

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/db"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
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

	svc := New(db.New(pool), password.New())

	t.Run("register, then log in with the email in another case", func(t *testing.T) {
		registered, err := svc.Register(ctx, "ana@example.pt", testPassword, RoleRider)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, registered.ID)
		assert.Equal(t, User{ID: registered.ID, Email: "ana@example.pt", Role: RoleRider}, registered)

		loggedIn, err := svc.Login(ctx, "Ana@Example.pt", testPassword)
		require.NoError(t, err)
		assert.Equal(t, registered, loggedIn)
	})

	t.Run("taken email, in another case", func(t *testing.T) {
		_, err := svc.Register(ctx, "bruno@example.pt", testPassword, RoleDriver)
		require.NoError(t, err)

		_, err = svc.Register(ctx, "Bruno@example.pt", testPassword, RoleRider)
		assert.ErrorIs(t, err, ErrEmailTaken)
	})

	t.Run("password breaks the rule", func(t *testing.T) {
		_, err := svc.Register(ctx, "carla@example.pt", "too short", RoleRider)
		require.ErrorIs(t, err, password.ErrInvalid)
		assert.Equal(t, "invalid password: must be at least 15 characters", err.Error())
	})

	t.Run("wrong password and unknown email fail the same way", func(t *testing.T) {
		_, err := svc.Register(ctx, "diana@example.pt", testPassword, RoleRider)
		require.NoError(t, err)

		_, err = svc.Login(ctx, "diana@example.pt", "not the right password")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		_, err = svc.Login(ctx, "nobody@example.pt", testPassword)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})
}
