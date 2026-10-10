package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
)

// fakePricing stands in for the Pricing service. The handler only depends on the
// generated client interface, so tests never need a real gRPC connection.
type fakePricing struct {
	resp *pricingv1.EstimateFareResponse
	err  error
	got  *pricingv1.EstimateFareRequest
}

func (f *fakePricing) EstimateFare(_ context.Context, req *pricingv1.EstimateFareRequest, _ ...grpc.CallOption) (*pricingv1.EstimateFareResponse, error) {
	f.got = req
	return f.resp, f.err
}

func TestFareEstimate(t *testing.T) {
	pricing := &fakePricing{resp: &pricingv1.EstimateFareResponse{FareCents: 750}}
	rec := serve(t, pricing, "/v1/fare-estimate?distance_meters=5000&duration_seconds=600")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"fare_cents": 750}`, rec.Body.String())

	require.NotNil(t, pricing.got)
	assert.Equal(t, int32(5000), pricing.got.GetDistanceMeters())
	assert.Equal(t, 10*time.Minute, pricing.got.GetDuration().AsDuration())
}

func TestFareEstimateRejectsBadQuery(t *testing.T) {
	pricing := &fakePricing{}
	rec := serve(t, pricing, "/v1/fare-estimate?distance_meters=five&duration_seconds=600")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Nil(t, pricing.got, "pricing should not be called")
}

func TestFareEstimateMapsPricingErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{
			name:     "validation error is shown to the client",
			err:      status.Error(codes.InvalidArgument, "distance_meters must be positive"),
			wantCode: http.StatusBadRequest,
			wantBody: "distance_meters must be positive\n",
		},
		{
			name:     "internal failure is hidden from the client",
			err:      status.Error(codes.Unavailable, "connection refused"),
			wantCode: http.StatusInternalServerError,
			wantBody: "internal error\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakePricing{err: tt.err}, "/v1/fare-estimate?distance_meters=0&duration_seconds=600")

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())
		})
	}
}

func serve(t *testing.T, pricing pricingv1.PricingServiceClient, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	NewHandler(pricing).FareEstimate(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}
