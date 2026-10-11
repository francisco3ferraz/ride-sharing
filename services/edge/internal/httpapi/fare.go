// Package httpapi holds the Edge's HTTP handlers. They call the internal services and
// shape responses; business rules stay in the services.
package httpapi

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

const pricingTimeout = 2 * time.Second

var _ api.StrictServerInterface = (*Handler)(nil)

type Handler struct {
	pricing pricingv1.PricingServiceClient
}

func NewHandler(pricing pricingv1.PricingServiceClient) *Handler {
	return &Handler{pricing: pricing}
}

func (h *Handler) GetFareEstimate(ctx context.Context, req api.GetFareEstimateRequestObject) (api.GetFareEstimateResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, pricingTimeout)
	defer cancel()

	estimate, err := h.pricing.EstimateFare(ctx, &pricingv1.EstimateFareRequest{
		DistanceMeters: req.Params.DistanceMeters,
		Duration:       durationpb.New(time.Duration(req.Params.DurationSeconds) * time.Second),
	})
	if err != nil {
		return nil, err
	}
	return api.GetFareEstimate200JSONResponse{FareCents: estimate.GetFareCents()}, nil
}
