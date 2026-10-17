package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

func TestGetMe(t *testing.T) {
	identity := &fakeIdentity{
		user: &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER},
	}
	verifier := &fakeVerifier{claims: testClaims()}

	rec := send(t, NewHandler(nil, identity, verifier), http.MethodGet, "/v1/me", "", testAccessToken)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"id": "`+testUserID+`", "email": "ana@example.pt", "role": "rider"}`, rec.Body.String())
	assert.Equal(t, []string{testAccessToken}, verifier.verified)
	require.NotNil(t, identity.gotGetUser)
	assert.Equal(t, testUserID, identity.gotGetUser.GetUserId())
}

func TestGetMeRequiresAccessToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		verifier *fakeVerifier
	}{
		{name: "no cookie", verifier: &fakeVerifier{claims: testClaims()}},
		// The exact detail below also proves the reason stays out of the response.
		{
			name:     "rejected token",
			token:    testAccessToken,
			verifier: &fakeVerifier{err: errors.New("token has invalid claims: token is expired")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{}
			rec := send(t, NewHandler(nil, identity, tt.verifier), http.MethodGet, "/v1/me", "", tt.token)

			assertProblem(t, rec, api.Problem{
				Type:   "about:blank",
				Title:  "Unauthorized",
				Status: http.StatusUnauthorized,
				Detail: new("security requirements failed: a valid access token is required"),
			})
			assert.Nil(t, identity.gotGetUser, "identity should not be called")
		})
	}
}

func TestGetMeMapsIdentityErrors(t *testing.T) {
	tests := []struct {
		name     string
		identity *fakeIdentity
		want     api.Problem
	}{
		{
			name:     "user no longer exists",
			identity: &fakeIdentity{err: status.Error(codes.NotFound, "user not found")},
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Unauthorized",
				Status: http.StatusUnauthorized,
				Detail: new("the access token's user no longer exists"),
			},
		},
		{
			name:     "internal failure",
			identity: &fakeIdentity{err: status.Error(codes.Internal, "get user: connection refused")},
			want:     api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError},
		},
		{
			name:     "id isn't a uuid",
			identity: &fakeIdentity{user: &identityv1.User{Id: "42", Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER}},
			want:     api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := &fakeVerifier{claims: testClaims()}
			rec := send(t, NewHandler(nil, tt.identity, verifier), http.MethodGet, "/v1/me", "", testAccessToken)

			assertProblem(t, rec, tt.want)
		})
	}
}

// Only a wiring mistake brings a request here without claims, so it's an error, and
// Identity isn't asked.
func TestGetMeWithoutClaims(t *testing.T) {
	identity := &fakeIdentity{}

	_, err := NewHandler(nil, identity, nil).GetMe(context.Background(), api.GetMeRequestObject{})

	require.Error(t, err)
	assert.Nil(t, identity.gotGetUser)
}
