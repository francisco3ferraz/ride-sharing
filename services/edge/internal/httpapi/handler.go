// Package httpapi holds the Edge's HTTP handlers. They call the internal services and
// shape responses; business rules stay in the services.
package httpapi

import (
	"context"
	"time"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/accesstoken"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/ratelimit"
)

var _ api.StrictServerInterface = (*Handler)(nil)

// TokenVerifier is what Handler needs from *accesstoken.Verifier, so tests can replace it.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (accesstoken.Claims, error)
}

// RateLimiter is what Handler needs from *ratelimit.Limiter, so tests can replace it.
type RateLimiter interface {
	Allow(ctx context.Context, name, key string, limit ratelimit.Limit) (bool, time.Duration)
}

// Limits are the rate limits Handler applies.
type Limits struct {
	IP         ratelimit.Limit
	User       ratelimit.Limit
	Login      ratelimit.Limit
	Register   ratelimit.Limit
	ResetIP    ratelimit.Limit
	ResetEmail ratelimit.Limit
}

type Handler struct {
	pricing  pricingv1.PricingServiceClient
	identity identityv1.IdentityServiceClient
	verifier TokenVerifier
	limiter  RateLimiter
	limits   Limits
}

func NewHandler(pricing pricingv1.PricingServiceClient, identity identityv1.IdentityServiceClient, verifier TokenVerifier, limiter RateLimiter, limits Limits) *Handler {
	return &Handler{pricing: pricing, identity: identity, verifier: verifier, limiter: limiter, limits: limits}
}
