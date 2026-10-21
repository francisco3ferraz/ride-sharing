package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/accesstoken"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

const (
	testVehicleBody = `{"make": "Toyota", "model": "Corolla", "colour": "White", "plate": "aa 01 bc"}`
	testVehicleJSON = `{"make": "Toyota", "model": "Corolla", "colour": "White", "plate": "AA-01-BC"}`
)

func testVehicle() *identityv1.Vehicle {
	return &identityv1.Vehicle{Make: "Toyota", Model: "Corolla", Colour: "White", Plate: "AA-01-BC"}
}

func driverClaims() accesstoken.Claims {
	claims := testClaims()
	claims.Role = "driver"
	return claims
}

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

func TestPutMyVehicle(t *testing.T) {
	tests := []struct {
		name       string
		created    bool
		wantStatus int
	}{
		{name: "created", created: true, wantStatus: http.StatusCreated},
		{name: "replaced", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{vehicle: testVehicle(), created: tt.created}
			verifier := &fakeVerifier{claims: driverClaims()}

			rec := send(t, NewHandler(nil, identity, verifier, &fakeLimiter{}, Limits{}), http.MethodPut, "/v1/me/vehicle", testVehicleBody, testAccessToken)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, testVehicleJSON, rec.Body.String())
			require.NotNil(t, identity.gotPutVehicle)
			assert.Equal(t, testUserID, identity.gotPutVehicle.GetUserId())
			assert.Equal(t, "aa 01 bc", identity.gotPutVehicle.GetVehicle().GetPlate())
			assert.Equal(t, "Corolla", identity.gotPutVehicle.GetVehicle().GetModel())
		})
	}
}

func TestPutMyVehicleMapsIdentityErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want api.Problem
	}{
		{
			name: "plate taken",
			err:  status.Error(codes.AlreadyExists, "plate is already registered to another driver"),
			want: api.Problem{Type: "about:blank", Title: "Conflict", Status: http.StatusConflict, Detail: new("plate is already registered to another driver")},
		},
		{
			name: "details break the rules",
			err:  status.Error(codes.InvalidArgument, "invalid vehicle: make isn't in the catalog"),
			want: api.Problem{Type: "about:blank", Title: "Bad Request", Status: http.StatusBadRequest, Detail: new("invalid vehicle: make isn't in the catalog")},
		},
		{
			name: "user no longer exists",
			err:  status.Error(codes.NotFound, "user not found"),
			want: api.Problem{Type: "about:blank", Title: "Unauthorized", Status: http.StatusUnauthorized, Detail: new("the access token's user no longer exists")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{err: tt.err}
			verifier := &fakeVerifier{claims: driverClaims()}

			rec := send(t, NewHandler(nil, identity, verifier, &fakeLimiter{}, Limits{}), http.MethodPut, "/v1/me/vehicle", testVehicleBody, testAccessToken)

			assertProblem(t, rec, tt.want)
		})
	}
}

func TestGetMyVehicle(t *testing.T) {
	identity := &fakeIdentity{vehicle: testVehicle()}
	verifier := &fakeVerifier{claims: driverClaims()}

	rec := send(t, NewHandler(nil, identity, verifier, &fakeLimiter{}, Limits{}), http.MethodGet, "/v1/me/vehicle", "", testAccessToken)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, testVehicleJSON, rec.Body.String())
	require.NotNil(t, identity.gotGetVehicle)
	assert.Equal(t, testUserID, identity.gotGetVehicle.GetUserId())
}

func TestGetMyVehicleBeforeOneIsSaved(t *testing.T) {
	identity := &fakeIdentity{err: status.Error(codes.NotFound, "no vehicle saved yet")}
	verifier := &fakeVerifier{claims: driverClaims()}

	rec := send(t, NewHandler(nil, identity, verifier, &fakeLimiter{}, Limits{}), http.MethodGet, "/v1/me/vehicle", "", testAccessToken)

	assertProblem(t, rec, api.Problem{Type: "about:blank", Title: "Not Found", Status: http.StatusNotFound, Detail: new("no vehicle saved yet")})
}

// The PUT's body is valid: the validator checks it before the handler can answer 403.
func TestMyVehicleIsForDriversOnly(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			identity := &fakeIdentity{vehicle: testVehicle()}
			verifier := &fakeVerifier{claims: testClaims()}
			body := ""
			if method == http.MethodPut {
				body = testVehicleBody
			}

			rec := send(t, NewHandler(nil, identity, verifier, &fakeLimiter{}, Limits{}), method, "/v1/me/vehicle", body, testAccessToken)

			assertProblem(t, rec, api.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: new("only drivers have a vehicle")})
			assert.Nil(t, identity.gotPutVehicle, "identity should not be called")
			assert.Nil(t, identity.gotGetVehicle, "identity should not be called")
		})
	}
}

func TestMyVehicleRequiresAccessToken(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			identity := &fakeIdentity{}

			rec := send(t, NewHandler(nil, identity, &fakeVerifier{claims: driverClaims()}, &fakeLimiter{}, Limits{}), method, "/v1/me/vehicle", "", "")

			assertProblem(t, rec, api.Problem{
				Type:   "about:blank",
				Title:  "Unauthorized",
				Status: http.StatusUnauthorized,
				Detail: new("security requirements failed: a valid access token is required"),
			})
		})
	}
}
