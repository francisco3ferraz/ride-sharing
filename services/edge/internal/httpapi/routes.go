package httpapi

import "net/http"

// Routes returns the Edge's HTTP routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /v1/fare-estimate", h.FareEstimate)
	return mux
}

// healthz reports that the Edge is up. It deliberately doesn't check other services:
// if it did, one failing service would take every Edge pod out of rotation.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
