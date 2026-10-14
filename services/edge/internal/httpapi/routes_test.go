package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

const loginBody = `{"email": "ana@example.pt", "password": "a long enough password"}`

func TestRejectsCrossOriginRequests(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{name: "from another site", header: http.Header{"Sec-Fetch-Site": {"cross-site"}}},
		{name: "from another host on our site", header: http.Header{"Sec-Fetch-Site": {"same-site"}}},
		{name: "old browser, from another host", header: http.Header{"Origin": {"https://grafana.ride-sharing.localhost"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{}
			rec := postWithHeader(t, identity, "/v1/auth/login", loginBody, tt.header)

			assertProblem(t, rec, api.Problem{
				Type:   "about:blank",
				Title:  "Forbidden",
				Status: http.StatusForbidden,
				Detail: new("cross-origin requests aren't allowed"),
			})
			assert.Nil(t, identity.gotLogin, "identity should not be called")
		})
	}
}

func TestAllowsSameOriginRequests(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{name: "from our own pages", header: http.Header{"Sec-Fetch-Site": {"same-origin"}}},
		{name: "started by the user", header: http.Header{"Sec-Fetch-Site": {"none"}}},
		// httptest.NewRequest's Host is example.com.
		{name: "old browser, from this host", header: http.Header{"Origin": {"https://example.com"}}},
		{name: "not a browser"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{
				user:   &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER},
				tokens: testTokens(),
			}
			rec := postWithHeader(t, identity, "/v1/auth/login", loginBody, tt.header)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.NotNil(t, identity.gotLogin, "identity should be called")
		})
	}
}
