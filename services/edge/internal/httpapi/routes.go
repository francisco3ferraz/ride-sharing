package httpapi

import (
	"fmt"
	"net/http"

	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// Routes returns the Edge's HTTP routes, traced with OpenTelemetry. The tracing
// middleware continues the trace Traefik started (from its traceparent header) and
// puts the span in each request's context, where the gRPC client picks it up.
func (h *Handler) Routes() (http.Handler, error) {
	spec, err := api.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}
	validate := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		// The spec's server is https://, but Traefik sends us plain HTTP.
		DoNotValidateServers: true,
		ErrorHandler:         validationError,
	})

	server := api.NewStrictHandlerWithOptions(h, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestError,
		ResponseErrorHandlerFunc: responseError,
	})
	apiMux := http.NewServeMux()
	// Bad query parameters are reported here; bad bodies go to RequestErrorHandlerFunc.
	api.HandlerWithOptions(server, api.StdHTTPServerOptions{BaseRouter: apiMux, ErrorHandlerFunc: requestError})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("/v1/", validate(apiMux))

	return otelhttp.NewHandler(mux, "edge",
		// Kubernetes probes /healthz every few seconds; those spans would flood Jaeger.
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
	), nil
}

// healthz reports that the Edge is up. It deliberately doesn't check other services:
// if it did, one failing service would take every Edge pod out of rotation.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
