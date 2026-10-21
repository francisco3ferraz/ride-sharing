package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

func TestResponseError(t *testing.T) {
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
			name: "failed login is shown to the client",
			err:  status.Error(codes.Unauthenticated, "invalid email or password"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Unauthorized",
				Status: http.StatusUnauthorized,
				Detail: new("invalid email or password"),
			},
		},
		{
			name: "a rider on a driver's route is shown why",
			err:  status.Error(codes.PermissionDenied, "only drivers have a vehicle"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Forbidden",
				Status: http.StatusForbidden,
				Detail: new("only drivers have a vehicle"),
			},
		},
		{
			name: "something missing is shown to the client",
			err:  status.Error(codes.NotFound, "no vehicle saved yet"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Not Found",
				Status: http.StatusNotFound,
				Detail: new("no vehicle saved yet"),
			},
		},
		{
			name: "conflict is shown to the client",
			err:  status.Error(codes.AlreadyExists, "email is already registered"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Conflict",
				Status: http.StatusConflict,
				Detail: new("email is already registered"),
			},
		},
		{
			name: "locked-out login is shown to the client",
			err:  status.Error(codes.ResourceExhausted, "too many failed logins: try again later"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Too Many Requests",
				Status: http.StatusTooManyRequests,
				Detail: new("too many failed logins: try again later"),
			},
		},
		{
			name: "unavailable service is a 503 that hides gRPC's message",
			err:  status.Error(codes.Unavailable, `connection error: desc = "transport: Error while dialing: dial tcp 10.96.0.20:50051: connect: connection refused"`),
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
		{
			name: "error that isn't from a service is hidden from the client",
			err:  errors.New("unexpected response type: string"),
			want: api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			responseError(rec, httptest.NewRequest(http.MethodGet, "/v1/fare-estimate", nil), tt.err)

			assertProblem(t, rec, tt.want)
		})
	}
}

func TestRequestError(t *testing.T) {
	rec := httptest.NewRecorder()
	requestError(rec, httptest.NewRequest(http.MethodGet, "/v1/fare-estimate", nil), &api.RequiredParamError{ParamName: "distance_meters"})

	assertProblem(t, rec, api.Problem{
		Type:   "about:blank",
		Title:  "Bad Request",
		Status: http.StatusBadRequest,
		Detail: new("Query argument distance_meters is required, but not found"),
	})
}

func TestValidationError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		code    int
		want    api.Problem
	}{
		{
			name:    "client error is shown to the client",
			message: `parameter "distance_meters" in query has an error: value is required but missing`,
			code:    http.StatusBadRequest,
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Bad Request",
				Status: http.StatusBadRequest,
				Detail: new(`parameter "distance_meters" in query has an error: value is required but missing`),
			},
		},
		{
			name:    "internal failure is hidden from the client",
			message: "error validating route: decoding failed",
			code:    http.StatusInternalServerError,
			want:    api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			validationError(rec, tt.message, tt.code)

			assertProblem(t, rec, tt.want)
		})
	}
}

// assertProblem checks that rec holds the problem want, with the RFC 9457 media type.
func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, want api.Problem) {
	t.Helper()
	assert.Equal(t, int(want.Status), rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))

	var got api.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, want, got)
}
