package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// No request in the API needs more, and the validator and the handlers each hold the
// whole body in memory.
const maxBodyBytes = 64 << 10

// The validator checks a format only once it's registered, and by default only byte,
// date and date-time are. The registry is global, so this runs once.
func init() {
	openapi3.DefineStringFormatValidator("email", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForEmail))
}

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
	mux.Handle("/v1/", rejectCrossOrigin(limitBody(validate(apiMux))))

	return otelhttp.NewHandler(mux, "edge",
		// Kubernetes probes /healthz every few seconds; those spans would flood Jaeger.
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
	), nil
}

// It goes outside limitBody, so a refused request's body is never read. A request with
// neither Sec-Fetch-Site nor Origin passes: only clients that aren't browsers send
// those, and they carry no victim's cookies.
func rejectCrossOrigin(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(crossOriginDenied))
	return protection.Handler(next)
}

func crossOriginDenied(w http.ResponseWriter, _ *http.Request) {
	writeProblem(w, http.StatusForbidden, "cross-origin requests aren't allowed")
}

// limitBody answers 413 for a body over maxBodyBytes. It reads the body before the
// validator does: the validator's error handler gets only a message, so it couldn't
// tell an oversized body from a malformed one.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body must be at most %d bytes", maxBodyBytes))
			return
		}
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "can't read request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// healthz reports that the Edge is up. It deliberately doesn't check other services:
// if it did, one failing service would take every Edge pod out of rotation.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
