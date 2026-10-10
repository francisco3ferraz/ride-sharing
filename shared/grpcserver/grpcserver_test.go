package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestServeStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	addr, served := serve(t, ctx, time.Second)

	resp, err := healthClient(t, addr).Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())

	cancel()
	assert.NoError(t, wait(t, served))
}

// A health Watch stream stays open until the client ends it, so GracefulStop alone
// would never return. Serve must close it once the stop timeout passes.
func TestServeClosesStuckRPCsAfterStopTimeout(t *testing.T) {
	const stopTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	addr, served := serve(t, ctx, stopTimeout)

	stream, err := healthClient(t, addr).Watch(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	_, err = stream.Recv() // the first status, so the server is now handling the stream
	require.NoError(t, err)

	start := time.Now()
	cancel()
	assert.NoError(t, wait(t, served))
	assert.GreaterOrEqual(t, time.Since(start), stopTimeout)
}

// serve runs a new Server on a free local port. It returns the address and a channel
// that receives what Serve returns.
func serve(t *testing.T, ctx context.Context, stopTimeout time.Duration) (string, <-chan error) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	served := make(chan error, 1)
	go func() { served <- New().Serve(ctx, lis, stopTimeout) }()
	return lis.Addr().String(), served
}

func healthClient(t *testing.T, addr string) healthpb.HealthClient {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	return healthpb.NewHealthClient(conn)
}

// wait returns what Serve returned, failing the test if it doesn't return in time.
func wait(t *testing.T, served <-chan error) error {
	t.Helper()
	select {
	case err := <-served:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Serve didn't return")
		return nil
	}
}
