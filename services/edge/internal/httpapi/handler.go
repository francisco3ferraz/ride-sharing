// Package httpapi holds the Edge's HTTP handlers. They call the internal services and
// shape responses; business rules stay in the services.
package httpapi

import (
	"context"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/accesstoken"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

var _ api.StrictServerInterface = (*Handler)(nil)

// TokenVerifier is what Handler needs from *accesstoken.Verifier, so tests can replace it.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (accesstoken.Claims, error)
}

type Handler struct {
	pricing  pricingv1.PricingServiceClient
	identity identityv1.IdentityServiceClient
	verifier TokenVerifier
}

func NewHandler(pricing pricingv1.PricingServiceClient, identity identityv1.IdentityServiceClient, verifier TokenVerifier) *Handler {
	return &Handler{pricing: pricing, identity: identity, verifier: verifier}
}
