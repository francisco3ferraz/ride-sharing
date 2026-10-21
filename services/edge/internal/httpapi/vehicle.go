package httpapi

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// Identity answers from memory.
const catalogTimeout = time.Second

// Identity checks the role again; checking here spares it a rider's call.
var errNotDriver = status.Error(codes.PermissionDenied, "only drivers have a vehicle")

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

func (h *Handler) PutMyVehicle(ctx context.Context, req api.PutMyVehicleRequestObject) (api.PutMyVehicleResponseObject, error) {
	claims, ok := claimsFrom(ctx)
	if !ok {
		return nil, errors.New("no access token claims in context")
	}
	if claims.Role != string(api.Driver) {
		return nil, errNotDriver
	}

	ctx, cancel := context.WithTimeout(ctx, userTimeout)
	defer cancel()

	resp, err := h.identity.PutVehicle(ctx, &identityv1.PutVehicleRequest{
		UserId:  claims.Subject,
		Vehicle: &identityv1.Vehicle{Make: req.Body.Make, Model: req.Body.Model, Colour: req.Body.Colour, Plate: req.Body.Plate},
	})
	if status.Code(err) == codes.NotFound {
		return nil, errUserGone
	}
	if err != nil {
		return nil, err
	}
	v := vehicleFromProto(resp.GetVehicle())
	if resp.GetCreated() {
		return api.PutMyVehicle201JSONResponse(v), nil
	}
	return api.PutMyVehicle200JSONResponse(v), nil
}

func (h *Handler) GetMyVehicle(ctx context.Context, _ api.GetMyVehicleRequestObject) (api.GetMyVehicleResponseObject, error) {
	claims, ok := claimsFrom(ctx)
	if !ok {
		return nil, errors.New("no access token claims in context")
	}
	if claims.Role != string(api.Driver) {
		return nil, errNotDriver
	}

	ctx, cancel := context.WithTimeout(ctx, userTimeout)
	defer cancel()

	// A driver without one gets Identity's NotFound, a 404.
	resp, err := h.identity.GetVehicle(ctx, &identityv1.GetVehicleRequest{UserId: claims.Subject})
	if err != nil {
		return nil, err
	}
	return api.GetMyVehicle200JSONResponse(vehicleFromProto(resp.GetVehicle())), nil
}

func vehicleFromProto(v *identityv1.Vehicle) api.Vehicle {
	return api.Vehicle{Make: v.GetMake(), Model: v.GetModel(), Colour: v.GetColour(), Plate: v.GetPlate()}
}
