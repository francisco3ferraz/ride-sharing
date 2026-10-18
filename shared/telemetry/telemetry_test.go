package telemetry

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	collmetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

// fakeCollector receives OTLP over gRPC, as the OpenTelemetry Collector does, and
// records the names of the spans and metrics it's sent.
type fakeCollector struct {
	colltracepb.UnimplementedTraceServiceServer
	collmetricpb.UnimplementedMetricsServiceServer
	spans   chan string
	metrics chan string
}

func (c *fakeCollector) Export(_ context.Context, req *colltracepb.ExportTraceServiceRequest) (*colltracepb.ExportTraceServiceResponse, error) {
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, span := range ss.GetSpans() {
				c.spans <- span.GetName()
			}
		}
	}
	return &colltracepb.ExportTraceServiceResponse{}, nil
}

// metricsService adapts fakeCollector to the metrics service, whose Export method has
// the same name as the trace service's.
type metricsService struct{ *fakeCollector }

func (m metricsService) Export(_ context.Context, req *collmetricpb.ExportMetricsServiceRequest) (*collmetricpb.ExportMetricsServiceResponse, error) {
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, metric := range sm.GetMetrics() {
				m.metrics <- metric.GetName()
			}
		}
	}
	return &collmetricpb.ExportMetricsServiceResponse{}, nil
}

func TestShutdownFlushesSpansAndMetrics(t *testing.T) {
	collector := &fakeCollector{spans: make(chan string, 10), metrics: make(chan string, 10)}
	srv := grpc.NewServer()
	colltracepb.RegisterTraceServiceServer(srv, collector)
	collmetricpb.RegisterMetricsServiceServer(srv, metricsService{collector})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+lis.Addr().String())

	shutdown, err := Setup(context.Background())
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(context.Background(), "test span")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.count")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))

	// Both arrived before shutdown returned, well before their periodic exports.
	assert.Equal(t, "test span", received(t, collector.spans))
	assert.Equal(t, "test.count", received(t, collector.metrics))
}

// received fails the test, rather than waiting, when nothing has arrived.
func received(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case name := <-ch:
		return name
	default:
		require.FailNow(t, "nothing was exported")
		return ""
	}
}
