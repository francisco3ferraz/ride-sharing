package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
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

// limitUser limits a logged-in user's requests, from however many addresses. It must
// go inside authenticate, which puts the claims in the context. Anonymous requests have
// only the per-address limit.
func (h *Handler) limitUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFrom(r.Context())
		if ok && !h.allow(w, r, "user", claims.Subject, h.limits.User) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitOperations is a strict-server middleware that applies the limits an operation
// has of its own. It runs after the validator, with the decoded request, so a
// malformed request isn't counted. That's fine: it never costs Identity a hash.
func (h *Handler) limitOperations(next api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		for _, l := range h.operationLimits(ctx, operationID, request) {
			if ok, retryAfter := h.limiter.Allow(ctx, l.name, l.key, l.limit); !ok {
				return nil, rateLimitedError{retryAfter: retryAfter}
			}
		}
		return next(ctx, w, r, request)
	}
}

type operationLimit struct {
	name  string
	key   string
	limit ratelimit.Limit
}

// operationLimits are checked in order, up to the first that's used up. operationID is
// the generated Go name, not the spec's operationId.
func (h *Handler) operationLimits(ctx context.Context, operationID string, request any) []operationLimit {
	ip := ipKey(ctx)
	switch operationID {
	case "Login":
		return []operationLimit{{name: "login", key: ip, limit: h.limits.Login}}
	case "RegisterUser":
		return []operationLimit{{name: "register", key: ip, limit: h.limits.Register}}
	case "RequestPasswordReset":
		// The generated code always passes this type for this operation.
		email := request.(api.RequestPasswordResetRequestObject).Body.Email
		// The address first, and the email only for requests it lets through. The other
		// way round, an address over its limit could keep using up an email's few resets
		// an hour, locking its owner out of them.
		return []operationLimit{
			{name: "reset", key: ip, limit: h.limits.ResetIP},
			{name: "reset-email", key: emailKey(email), limit: h.limits.ResetEmail},
		}
	default:
		return nil
	}
}

// emailKey keeps addresses out of Valkey's keys, which SCAN and MONITOR show. The hash
// isn't secret, since anyone with a list of addresses can hash them too, but these keys
// expire within the hour. Trimmed and lowercased first, as Identity's citext compares
// emails, so variants of one address share its limit.
func emailKey(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

// rateLimitedError is how limitOperations reports a used-up limit, so responseError
// writes its 429 like every other response to an error.
type rateLimitedError struct {
	retryAfter time.Duration
}

func (rateLimitedError) Error() string {
	return "rate limit used up"
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
