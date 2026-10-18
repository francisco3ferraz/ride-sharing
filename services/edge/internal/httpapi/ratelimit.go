package httpapi

import (
	"context"
	"net/http"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/clientip"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/ratelimit"
)

// limitIP limits every /v1 request by the client's address. It runs before anything
// else, so a flood is turned away before it costs any other work.
func (h *Handler) limitIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.allow(w, r, "ip", ipKey(r.Context()), h.limits.IP) {
			next.ServeHTTP(w, r)
		}
	})
}

// allow checks one limit, and answers 429 when it's used up.
func (h *Handler) allow(w http.ResponseWriter, r *http.Request, name, key string, limit ratelimit.Limit) bool {
	ok, retryAfter := h.limiter.Allow(r.Context(), name, key, limit)
	if !ok {
		tooManyRequests(w, retryAfter)
	}
	return ok
}

// ipKey is what a client is limited by. One IPv6 subscriber usually gets a whole /64,
// so limiting each address alone would let them rotate through it. Requests that
// didn't come through Traefik share one key: never no key, which would mean no limit.
func ipKey(ctx context.Context) string {
	addr, ok := clientip.FromContext(ctx)
	switch {
	case !ok:
		return "unknown"
	case addr.Is6():
		network, _ := addr.Prefix(64) // fails only for more bits than the address has
		return network.String()
	default:
		return addr.String()
	}
}
