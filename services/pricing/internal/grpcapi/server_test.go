package grpcapi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
)

func TestEstimateFare(t *testing.T) {
	srv := &Server{}

	resp, err := srv.EstimateFare(context.Background(), &pricingv1.EstimateFareRequest{
		DistanceMeters: 5_000,
		Duration:       durationpb.New(10 * time.Minute),
	})

	require.NoError(t, err)
	assert.Equal(t, int64(750), resp.GetFareCents())
}

func TestEstimateFareRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		req  *pricingv1.EstimateFareRequest
	}{
		{
			name: "missing duration",
			req:  &pricingv1.EstimateFareRequest{DistanceMeters: 5_000},
		},
		{
			name: "zero distance",
			req:  &pricingv1.EstimateFareRequest{Duration: durationpb.New(time.Minute)},
		},
		{
			name: "negative duration",
			req: &pricingv1.EstimateFareRequest{
				DistanceMeters: 5_000,
				Duration:       durationpb.New(-time.Minute),
			},
		},
	}

	srv := &Server{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := srv.EstimateFare(context.Background(), tt.req)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}
