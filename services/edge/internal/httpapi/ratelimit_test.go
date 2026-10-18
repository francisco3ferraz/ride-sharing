package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	pricingv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/pricing/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/ratelimit"
)

type limitCheck struct {
	name  string
	key   string
	limit ratelimit.Limit
}

// fakeLimiter stands in for *ratelimit.Limiter. It allows every request, except for
// the limits named in deny, which answer with their retry-after. checks records every
// check, in order.
type fakeLimiter struct {
	deny   map[string]time.Duration
	checks []limitCheck
}

func (f *fakeLimiter) Allow(_ context.Context, name, key string, limit ratelimit.Limit) (bool, time.Duration) {
	f.checks = append(f.checks, limitCheck{name: name, key: key, limit: limit})
	if retryAfter, denied := f.deny[name]; denied {
		return false, retryAfter
	}
	return true, 0
}

var testLimits = Limits{
	IP:   ratelimit.Limit{Rate: 20, Burst: 40, Period: time.Second},
	User: ratelimit.Limit{Rate: 10, Burst: 20, Period: time.Second},
}

const fareTarget = "/v1/fare-estimate?distance_meters=5000&duration_seconds=600"

func TestLimitsEachClientAddress(t *testing.T) {
	tests := []struct {
		name         string
		forwardedFor string
		wantKey      string
	}{
		{name: "IPv4", forwardedFor: "198.51.100.4, 203.0.113.7", wantKey: "203.0.113.7"},
		{name: "IPv6, by its /64", forwardedFor: "2001:db8:1:2:3:4:5:6", wantKey: "2001:db8:1:2::/64"},
		{name: "not through Traefik", wantKey: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := &fakeLimiter{}
			pricing := &fakePricing{resp: &pricingv1.EstimateFareResponse{FareCents: 750}}
			req := httptest.NewRequest(http.MethodGet, fareTarget, nil)
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}

			rec := serveWithLimits(t, NewHandler(pricing, nil, nil, limiter, testLimits), req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, []limitCheck{{name: "ip", key: tt.wantKey, limit: testLimits.IP}}, limiter.checks)
		})
	}
}

func TestOverTheLimitIsTooManyRequests(t *testing.T) {
	tests := []struct {
		retryAfter time.Duration
		want       string
	}{
		{retryAfter: 1500 * time.Millisecond, want: "2"},
		{retryAfter: 10 * time.Millisecond, want: "1"},
		{retryAfter: time.Minute, want: "60"},
	}

	for _, tt := range tests {
		t.Run(tt.retryAfter.String(), func(t *testing.T) {
			limiter := &fakeLimiter{deny: map[string]time.Duration{"ip": tt.retryAfter}}
			pricing := &fakePricing{}
			req := httptest.NewRequest(http.MethodGet, fareTarget, nil)

			rec := serveWithLimits(t, NewHandler(pricing, nil, nil, limiter, testLimits), req)

			assertProblem(t, rec, api.Problem{Type: "about:blank", Title: "Too Many Requests", Status: http.StatusTooManyRequests})
			assert.Equal(t, tt.want, rec.Header().Get("Retry-After"))
			assert.Nil(t, pricing.got, "pricing should not be called")
		})
	}
}

func TestLimitsEachLoggedInUser(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		verifier   *fakeVerifier
		wantChecks []limitCheck
	}{
		{
			name:     "logged in",
			token:    testAccessToken,
			verifier: &fakeVerifier{claims: testClaims()},
			wantChecks: []limitCheck{
				{name: "ip", key: "unknown", limit: testLimits.IP},
				{name: "user", key: testUserID, limit: testLimits.User},
			},
		},
		{
			name:       "token that fails verification",
			token:      testAccessToken,
			verifier:   &fakeVerifier{err: errors.New("token is expired")},
			wantChecks: []limitCheck{{name: "ip", key: "unknown", limit: testLimits.IP}},
		},
		{
			name:       "anonymous",
			verifier:   &fakeVerifier{},
			wantChecks: []limitCheck{{name: "ip", key: "unknown", limit: testLimits.IP}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := &fakeLimiter{}
			identity := &fakeIdentity{user: &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER}}

			send(t, NewHandler(nil, identity, tt.verifier, limiter, testLimits), http.MethodGet, "/v1/me", "", tt.token)

			assert.Equal(t, tt.wantChecks, limiter.checks)
		})
	}
}

func TestOverTheUserLimitIsTooManyRequests(t *testing.T) {
	limiter := &fakeLimiter{deny: map[string]time.Duration{"user": time.Second}}
	identity := &fakeIdentity{}

	rec := send(t, NewHandler(nil, identity, &fakeVerifier{claims: testClaims()}, limiter, testLimits), http.MethodGet, "/v1/me", "", testAccessToken)

	assertProblem(t, rec, api.Problem{Type: "about:blank", Title: "Too Many Requests", Status: http.StatusTooManyRequests})
	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
	assert.Nil(t, identity.gotGetUser, "identity should not be called")
}

func TestHealthzIsNotLimited(t *testing.T) {
	limiter := &fakeLimiter{deny: map[string]time.Duration{"ip": time.Second}}

	rec := serveWithLimits(t, NewHandler(nil, nil, nil, limiter, testLimits), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, limiter.checks)
}

func serveWithLimits(t *testing.T, h *Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	routes, err := h.Routes()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec
}
