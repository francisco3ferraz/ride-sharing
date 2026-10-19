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
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
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
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.Nil(t, pricing.got, "pricing should not be called")
}

func TestFareEstimateMapsPricingErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want api.Problem
	}{
		{
			name: "validation error is shown to the client",
			err:  status.Error(codes.InvalidArgument, "distance_meters must be positive"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Bad Request",
				Status: http.StatusBadRequest,
				Detail: new("distance_meters must be positive"),
			},
		},
		{
			name: "pricing unavailable is a 503",
			err:  status.Error(codes.Unavailable, "connection refused"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Service Unavailable",
				Status: http.StatusServiceUnavailable,
				Detail: new("the service is busy or unavailable: try again in a moment"),
			},
		},
		{
			name: "internal failure is hidden from the client",
			err:  status.Error(codes.Internal, "get quote: connection refused"),
			want: api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakePricing{err: tt.err}, "/v1/fare-estimate?distance_meters=0&duration_seconds=600")

			assertProblem(t, rec, tt.want)
		})
	}
}

func TestFareEstimateRejectsOtherMethods(t *testing.T) {
	routes, err := NewHandler(&fakePricing{}, nil, nil, &fakeLimiter{}, Limits{}).Routes()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/fare-estimate?distance_meters=5000&duration_seconds=600", nil)
	routes.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

func TestUnknownPathIsNotFound(t *testing.T) {
	rec := serve(t, &fakePricing{}, "/v1/nope")

	assertProblem(t, rec, api.Problem{
		Type:   "about:blank",
		Title:  "Not Found",
		Status: http.StatusNotFound,
		Detail: new("no matching operation was found"),
	})
}

func TestHealthz(t *testing.T) {
	rec := serve(t, &fakePricing{}, "/healthz")

	assert.Equal(t, http.StatusOK, rec.Code)
}

// serve sends a GET request through the real routes, so tests cover routing too.
func serve(t *testing.T, pricing pricingv1.PricingServiceClient, target string) *httptest.ResponseRecorder {
	t.Helper()
	routes, err := NewHandler(pricing, nil, nil, &fakeLimiter{}, Limits{}).Routes()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}
