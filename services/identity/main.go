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

	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/config"
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

	// `identity migrate` is the init container's command (ADR 0007). It applies the
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

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}

	srv := grpcserver.New()
	return srv.Serve(ctx, lis, shutdownTimeout)
}
