// Package grpcserver runs a service's gRPC server: traced, serving the standard health
// service, and stopped gracefully.
package grpcserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Server is a gRPC server with tracing and the standard health service. Generated
// Register…Server functions accept it, like a *grpc.Server.
type Server struct {
	grpc   *grpc.Server
	health *health.Server
}

func New() *Server {
	srv := grpc.NewServer(
		// Traces every RPC except Kubernetes' health checks, which would flood Jaeger.
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(filters.Not(filters.HealthCheck())))),
	)
	healthSrv := health.NewServer() // reports SERVING until Serve starts shutting down
	healthpb.RegisterHealthServer(srv, healthSrv)
	return &Server{grpc: srv, health: healthSrv}
}

// RegisterService implements grpc.ServiceRegistrar.
func (s *Server) RegisterService(desc *grpc.ServiceDesc, impl any) {
	s.grpc.RegisterService(desc, impl)
}

// Serve handles RPCs on lis until ctx is cancelled, then shuts down: it reports
// NOT_SERVING, lets in-flight RPCs finish, and closes every connection once stopTimeout
// passes. It returns nil after a shutdown, or the error that stopped it serving.
func (s *Server) Serve(ctx context.Context, lis net.Listener, stopTimeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("grpc server listening on %s", lis.Addr())
		serveErr <- s.grpc.Serve(lis)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Print("shutting down")
	// Report NOT_SERVING first, so anything checking our health stops sending new work.
	s.health.Shutdown()
	s.gracefulStop(stopTimeout)
	log.Print("stopped")
	return nil
}

// gracefulStop lets in-flight RPCs finish, but closes every connection once the
// timeout passes. GracefulStop on its own would wait forever for a stuck RPC.
func (s *Server) gracefulStop(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.Print("graceful stop timed out, closing connections")
		s.grpc.Stop()
	}
}
