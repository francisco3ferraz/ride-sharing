package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Postgres can still be starting on the first `tilt up`, so migrate keeps
// trying to reach it for a while before it gives up.
const (
	databaseWaitTimeout = time.Minute
	pingTimeout         = 2 * time.Second
	pingRetryInterval   = 2 * time.Second
)

// Compiled into the binary, so the image always carries the schema its code expects.
//
//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// migrate applies the migrations the database doesn't have yet. It runs as the
// Deployment's init container, before the server starts.
func migrate(ctx context.Context) error {
	// goose needs a *sql.DB, so this uses pgx through database/sql. The empty
	// connection string makes pgx read the standard PG* environment variables.
	db, err := sql.Open("pgx", "")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	if err := waitForDatabase(ctx, db); err != nil {
		return err
	}

	// goose looks for migration files at the root of the filesystem it's given.
	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations folder: %w", err)
	}
	// Holds a Postgres advisory lock while migrating, so pods starting together
	// apply each migration once.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if len(results) == 0 {
		log.Print("schema is up to date")
	}
	for _, result := range results {
		log.Print(result)
	}
	return nil
}

// waitForDatabase pings db until it answers, for up to databaseWaitTimeout.
func waitForDatabase(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(databaseWaitTimeout)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
		err := db.PingContext(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("reach database: %w", err)
		}
		log.Printf("database not reachable yet, retrying: %v", err)

		select {
		case <-ctx.Done():
			return fmt.Errorf("reach database: %w", ctx.Err())
		case <-time.After(pingRetryInterval):
		}
	}
}
