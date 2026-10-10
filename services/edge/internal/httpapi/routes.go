package httpapi

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Routes returns the Edge's HTTP routes, traced with OpenTelemetry. The tracing
// middleware continues the trace Traefik started (from its traceparent header) and
// puts the span in each request's context, where the gRPC client picks it up.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /v1/fare-estimate", h.FareEstimate)

	return otelhttp.NewHandler(mux, "edge",
		// Kubernetes probes /healthz every few seconds; those spans would flood Jaeger.
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
	)
}

// healthz reports that the Edge is up. It deliberately doesn't check other services:
// if it did, one failing service would take every Edge pod out of rotation.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
