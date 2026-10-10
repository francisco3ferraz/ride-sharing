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

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/pricing/internal/config"
	"github.com/francisco3ferraz/ride-sharing/services/pricing/internal/grpcapi"
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

	srv := grpcserver.New()
	pricingv1.RegisterPricingServiceServer(srv, &grpcapi.Server{})
	return srv.Serve(ctx, lis, shutdownTimeout)
}
