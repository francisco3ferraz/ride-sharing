// Package grpcapi exposes the Pricing service over gRPC. It validates requests and
// translates between protobuf messages and the fare package.
package grpcapi

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/pricing/internal/fare"
)

// Server implements pricingv1.PricingServiceServer.
type Server struct {
	pricingv1.UnimplementedPricingServiceServer
}

func (s *Server) EstimateFare(_ context.Context, req *pricingv1.EstimateFareRequest) (*pricingv1.EstimateFareResponse, error) {
	if req.GetDistanceMeters() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "distance_meters must be positive")
	}
	if req.GetDuration() == nil {
		return nil, status.Error(codes.InvalidArgument, "duration is required")
	}
	duration := req.GetDuration().AsDuration()
	if duration <= 0 {
		return nil, status.Error(codes.InvalidArgument, "duration must be positive")
	}

	return &pricingv1.EstimateFareResponse{
		FareCents: fare.Estimate(req.GetDistanceMeters(), duration),
	}, nil
}
