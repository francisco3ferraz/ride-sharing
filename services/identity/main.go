package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/accesstoken"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/account"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/config"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/email"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/grpcapi"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/httpapi"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
	"github.com/francisco3ferraz/ride-sharing/shared/grpcserver"
	"github.com/francisco3ferraz/ride-sharing/shared/telemetry"
)

// Kubernetes kills the pod 30 seconds after SIGTERM, so stopping the server and then
// flushing telemetry must together finish well before that.
const (
	shutdownTimeout          = 20 * time.Second
	telemetryShutdownTimeout = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// `identity migrate` is the init container's command. It applies the
	// migrations and exits, before any server or telemetry is set up.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return migrate(ctx)
	}

	shutdownTelemetry, err := telemetry.Setup(ctx)
	if err != nil {
		return fmt.Errorf("set up telemetry: %w", err)
	}
	// Deferred first, so it runs last: the server has stopped and its final spans exist.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			log.Printf("shut down telemetry: %v", err)
		}
	}()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// A missing or bad key fails here, as a crashing pod, instead of failing every login.
	key, err := accesstoken.LoadKey(cfg.JWTKeyPath)
	if err != nil {
		return err
	}

	pool, err := newPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	signer := accesstoken.New(key, time.Now)
	jwks, err := signer.JWKS()
	if err != nil {
		return err
	}
	mailer, err := email.New(cfg.SMTPHost, cfg.SMTPPort, cfg.MailFrom, cfg.WebURL)
	if err != nil {
		return err
	}
	accounts := account.New(pool, password.New(), signer, mailer)

	grpcLis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}
	httpLis, err := net.Listen("tcp", cfg.HTTPListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPListenAddr, err)
	}

	grpcSrv := grpcserver.New()
	identityv1.RegisterIdentityServiceServer(grpcSrv, grpcapi.New(accounts))
	httpSrv := &http.Server{
		Handler: httpapi.Routes(jwks),
		// Stops a client from holding a connection open by sending headers very slowly.
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ctx is cancelled when either server stops, so the other one shuts down too. Both
	// shut down at once, so together they still take at most shutdownTimeout.
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return grpcSrv.Serve(ctx, grpcLis, shutdownTimeout)
	})
	g.Go(func() error {
		log.Printf("http server listening on %s", httpLis.Addr())
		if err := httpSrv.Serve(httpLis); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve http: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		// Not ctx, which is already cancelled: Shutdown would drop requests in flight.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down http: %w", err)
		}
		return nil
	})
	return g.Wait()
}

// newPool doesn't connect yet: connections open on the first query, so a wrong setting
// only shows up then. The init container has already connected with the same settings.
func newPool(ctx context.Context) (*pgxpool.Pool, error) {
	// The empty connection string makes pgx read the standard PG* environment variables.
	poolConfig, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	// Traces every query with its SQL. Never add WithIncludeQueryParameters: the
	// parameters hold emails and password hashes, which would end up in Jaeger.
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	return pool, nil
}
