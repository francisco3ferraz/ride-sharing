// Package telemetry sets up OpenTelemetry tracing and metrics for a service. Where they
// go and the service's name come from the standard OTEL_EXPORTER_OTLP_ENDPOINT and
// OTEL_SERVICE_NAME environment variables, which the SDK reads itself.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup installs the global tracer and meter providers, and the propagator. It returns
// a function that flushes pending spans and metrics and stops both; call it after the
// service's servers have stopped, so their last spans are sent.
func Setup(ctx context.Context) (shutdown func(context.Context) error, err error) {
	// Both connect lazily, on the first export, so Setup doesn't need the Collector to
	// be up.
	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		// So traces show up within a second while developing. The default is 5 seconds.
		sdktrace.WithBatcher(traceExporter, sdktrace.WithBatchTimeout(time.Second)),
	)
	otel.SetTracerProvider(tracerProvider)

	meterProvider := sdkmetric.NewMeterProvider(
		// So metrics show up within seconds while developing. The default is a minute.
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(10*time.Second))),
	)
	otel.SetMeterProvider(meterProvider)

	// W3C Trace Context carries the trace ID from one service to the next, in the
	// traceparent header. Without it, every service would start its own trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Shutting down a provider flushes what it holds, then shuts down its exporter.
	return func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}, nil
}
