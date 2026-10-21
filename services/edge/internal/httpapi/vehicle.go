package httpapi

import (
	"context"
	"time"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// Identity answers from memory.
const catalogTimeout = time.Second

// GetVehicleCatalog answers with the makes, models and colours Identity accepts.
func (h *Handler) GetVehicleCatalog(ctx context.Context, _ api.GetVehicleCatalogRequestObject) (api.GetVehicleCatalogResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()

	resp, err := h.identity.GetVehicleCatalog(ctx, &identityv1.GetVehicleCatalogRequest{})
	if err != nil {
		return nil, err
	}
	// Never nil, which JSON encodes as null where the spec promises an array.
	catalog := api.VehicleCatalog{
		Makes:   make([]api.VehicleMake, 0, len(resp.GetMakes())),
		Colours: append([]string{}, resp.GetColours()...),
	}
	for _, m := range resp.GetMakes() {
		catalog.Makes = append(catalog.Makes, api.VehicleMake{Name: m.GetName(), Models: append([]string{}, m.GetModels()...)})
	}
	return api.GetVehicleCatalog200JSONResponse(catalog), nil
}
