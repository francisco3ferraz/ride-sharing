package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/config"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/httpapi"
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

	// NewClient doesn't connect yet. It connects on the first call and reconnects
	// on its own, so one long-lived connection serves every request.
	conn, err := grpc.NewClient(
		cfg.PricingAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Sends the current trace ID to Pricing with every call.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("create pricing client: %w", err)
	}
	// Deferred, so it runs after the HTTP server below has finished its requests.
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("close pricing connection: %v", err)
		}
	}()

	routes, err := httpapi.NewHandler(pricingv1.NewPricingServiceClient(conn)).Routes()
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}

	srv := &http.Server{
		Handler: routes,
		// Stops a client from holding a connection open by sending headers very slowly.
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("edge listening on %s", cfg.ListenAddr)
		serveErr <- srv.Serve(lis)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	log.Print("stopped")
	return nil
}
