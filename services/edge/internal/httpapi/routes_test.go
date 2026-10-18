package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/clientip"
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

func TestFindClientIP(t *testing.T) {
	tests := []struct {
		name         string
		forwardedFor string
		wantAddr     netip.Addr
		wantFound    bool
	}{
		{name: "through Traefik", forwardedFor: "10.0.0.1, 203.0.113.7", wantAddr: netip.MustParseAddr("203.0.113.7"), wantFound: true},
		{name: "not through Traefik"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			var gotAddr netip.Addr
			var found bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				gotAddr, found = clientip.FromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}
			rec := httptest.NewRecorder()

			findClientIP(next).ServeHTTP(rec, req)

			assert.Equal(t, 1, calls, "next should be called once")
			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.Equal(t, tt.wantFound, found)
			assert.Equal(t, tt.wantAddr, gotAddr)
		})
	}
}
