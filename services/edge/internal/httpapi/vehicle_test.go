package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

func TestGetVehicleCatalog(t *testing.T) {
	tests := []struct {
		name    string
		catalog *identityv1.GetVehicleCatalogResponse
		want    string
	}{
		{
			name: "in identity's order",
			catalog: &identityv1.GetVehicleCatalogResponse{
				Makes: []*identityv1.VehicleMake{
					{Name: "Toyota", Models: []string{"Corolla", "C-HR"}},
					{Name: "Citroën", Models: []string{"C4"}},
				},
				Colours: []string{"White", "Black"},
			},
			want: `{
				"makes": [
					{"name": "Toyota", "models": ["Corolla", "C-HR"]},
					{"name": "Citroën", "models": ["C4"]}
				],
				"colours": ["White", "Black"]
			}`,
		},
		{
			name:    "empty lists are arrays, not null",
			catalog: &identityv1.GetVehicleCatalogResponse{Makes: []*identityv1.VehicleMake{{Name: "Toyota"}}},
			want:    `{"makes": [{"name": "Toyota", "models": []}], "colours": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{catalog: tt.catalog}

			// No access token: the route is public.
			rec := send(t, NewHandler(nil, identity, &fakeVerifier{}, &fakeLimiter{}, Limits{}), http.MethodGet, "/v1/vehicle-catalog", "", "")

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, tt.want, rec.Body.String())
			assert.NotNil(t, identity.gotGetVehicleCatalog)
		})
	}
}

func TestGetVehicleCatalogWhileIdentityIsDown(t *testing.T) {
	identity := &fakeIdentity{err: status.Error(codes.Unavailable, "connection refused")}

	rec := send(t, NewHandler(nil, identity, &fakeVerifier{}, &fakeLimiter{}, Limits{}), http.MethodGet, "/v1/vehicle-catalog", "", "")

	assertProblem(t, rec, api.Problem{
		Type:   "about:blank",
		Title:  "Service Unavailable",
		Status: http.StatusServiceUnavailable,
		Detail: new(unavailableDetail),
	})
}
