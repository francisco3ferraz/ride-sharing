// Package httpapi holds the Edge's HTTP handlers. Handlers parse requests, call the
// internal services and shape responses. Business rules stay in the services.
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
)

const pricingTimeout = 2 * time.Second

type Handler struct {
	pricing pricingv1.PricingServiceClient
}

func NewHandler(pricing pricingv1.PricingServiceClient) *Handler {
	return &Handler{pricing: pricing}
}

type fareEstimateResponse struct {
	FareCents int64 `json:"fare_cents"`
}

// FareEstimate handles GET /v1/fare-estimate?distance_meters=…&duration_seconds=…
func (h *Handler) FareEstimate(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Parsing as 32-bit keeps both values in range for the int32 proto field and for
	// the conversion to time.Duration below.
	distanceMeters, err := strconv.ParseInt(query.Get("distance_meters"), 10, 32)
	if err != nil {
		http.Error(w, "distance_meters must be a whole number", http.StatusBadRequest)
		return
	}
	durationSeconds, err := strconv.ParseInt(query.Get("duration_seconds"), 10, 32)
	if err != nil {
		http.Error(w, "duration_seconds must be a whole number", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), pricingTimeout)
	defer cancel()

	resp, err := h.pricing.EstimateFare(ctx, &pricingv1.EstimateFareRequest{
		DistanceMeters: int32(distanceMeters),
		Duration:       durationpb.New(time.Duration(durationSeconds) * time.Second),
	})
	if err != nil {
		// Pricing owns the rules, so its validation message is safe to pass on.
		// Anything else is an internal failure, and its details stay in our logs.
		if status.Code(err) == codes.InvalidArgument {
			http.Error(w, status.Convert(err).Message(), http.StatusBadRequest)
			return
		}
		log.Printf("estimate fare: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, fareEstimateResponse{FareCents: resp.GetFareCents()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}
