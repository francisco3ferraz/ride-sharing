// Package telemetry sets up OpenTelemetry tracing for a service. Where traces go
// and the service's name come from the standard OTEL_EXPORTER_OTLP_ENDPOINT and
// OTEL_SERVICE_NAME environment variables, which the SDK reads itself.
package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup installs the global tracer provider and propagator. It returns a function
// that flushes pending spans and stops tracing; call it after the service's servers
// have stopped, so their last spans are sent.
func Setup(ctx context.Context) (shutdown func(context.Context) error, err error) {
	// Connects lazily, on the first export, so Setup doesn't need the Collector to be up.
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		// So traces show up within a second while developing. The default is 5 seconds.
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Second)),
	)
	otel.SetTracerProvider(tracerProvider)

	// W3C Trace Context carries the trace ID from one service to the next, in the
	// traceparent header. Without it, every service would start its own trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Shutting down the provider flushes its batch, then shuts down the exporter.
	return tracerProvider.Shutdown, nil
}
