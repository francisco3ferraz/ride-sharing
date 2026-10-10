package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/pricing/internal/config"
	"github.com/francisco3ferraz/ride-sharing/services/pricing/internal/grpcapi"
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

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}

	srv := grpc.NewServer(
		// Traces every RPC except Kubernetes' health checks, which would flood Jaeger.
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(filters.Not(filters.HealthCheck())))),
	)
	healthSrv := health.NewServer() // reports SERVING until Shutdown is called
	healthpb.RegisterHealthServer(srv, healthSrv)
	pricingv1.RegisterPricingServiceServer(srv, &grpcapi.Server{})

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("pricing listening on %s", cfg.ListenAddr)
		serveErr <- srv.Serve(lis)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Print("shutting down")
	// Report NOT_SERVING first, so anything checking our health stops sending new work.
	healthSrv.Shutdown()
	gracefulStop(srv, shutdownTimeout)
	log.Print("stopped")
	return nil
}

// gracefulStop lets in-flight RPCs finish, but closes every connection once the
// timeout passes. GracefulStop on its own would wait forever for a stuck RPC.
func gracefulStop(srv *grpc.Server, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.Print("graceful stop timed out, closing connections")
		srv.Stop()
	}
}
