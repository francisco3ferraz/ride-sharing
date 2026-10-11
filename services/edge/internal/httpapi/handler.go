// Package httpapi holds the Edge's HTTP handlers. They call the internal services and
// shape responses; business rules stay in the services.
package httpapi

import (
	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

var _ api.StrictServerInterface = (*Handler)(nil)

type Handler struct {
	pricing  pricingv1.PricingServiceClient
	identity identityv1.IdentityServiceClient
}

func NewHandler(pricing pricingv1.PricingServiceClient, identity identityv1.IdentityServiceClient) *Handler {
	return &Handler{pricing: pricing, identity: identity}
}
