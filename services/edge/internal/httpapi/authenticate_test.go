package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/accesstoken"
)

// fakeVerifier stands in for *accesstoken.Verifier. It fails with err when it's set,
// and otherwise returns claims. verified records every token it was given.
type fakeVerifier struct {
	claims   accesstoken.Claims
	err      error
	verified []string
}

func (f *fakeVerifier) Verify(_ context.Context, token string) (accesstoken.Claims, error) {
	f.verified = append(f.verified, token)
	if f.err != nil {
		return accesstoken.Claims{}, f.err
	}
	return f.claims, nil
}

func testClaims() accesstoken.Claims {
	return accesstoken.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: testUserID}, Role: "rider"}
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name         string
		cookie       string
		verifier     *fakeVerifier
		wantVerified []string
		wantClaims   bool
	}{
		{name: "no cookie", verifier: &fakeVerifier{claims: testClaims()}},
		{
			name:         "rejected token",
			cookie:       testAccessToken,
			verifier:     &fakeVerifier{err: errors.New("token is expired")},
			wantVerified: []string{testAccessToken},
		},
		{
			name:         "valid token",
			cookie:       testAccessToken,
			verifier:     &fakeVerifier{claims: testClaims()},
			wantVerified: []string{testAccessToken},
			wantClaims:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			var gotClaims accesstoken.Claims
			var hasClaims bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				gotClaims, hasClaims = claimsFrom(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: accessTokenCookie, Value: tt.cookie})
			}
			rec := httptest.NewRecorder()

			NewHandler(nil, nil, tt.verifier).authenticate(next).ServeHTTP(rec, req)

			// Whatever the token, the request reaches next exactly once, and only next
			// writes the response.
			assert.Equal(t, 1, calls)
			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.Equal(t, tt.wantVerified, tt.verifier.verified)
			assert.Equal(t, tt.wantClaims, hasClaims)
			if tt.wantClaims {
				assert.Equal(t, testClaims(), gotClaims)
			}
		})
	}
}

func TestRequireClaims(t *testing.T) {
	withToken := withClaims(context.Background(), testClaims())
	tests := []struct {
		name    string
		ctx     context.Context
		scheme  string
		wantErr bool
	}{
		{name: "claims", ctx: withToken, scheme: cookieAuthScheme},
		{name: "no claims", ctx: context.Background(), scheme: cookieAuthScheme, wantErr: true},
		{name: "another scheme", ctx: withToken, scheme: "BearerAuth", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireClaims(tt.ctx, &openapi3filter.AuthenticationInput{SecuritySchemeName: tt.scheme})

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// A stale cookie mustn't lock anyone out of the routes that start a session.
func TestPublicRoutesIgnoreRejectedTokens(t *testing.T) {
	identity := &fakeIdentity{
		user:   &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER},
		tokens: testTokens(),
	}
	verifier := &fakeVerifier{err: errors.New("token is expired")}

	rec := send(t, NewHandler(nil, identity, verifier), http.MethodPost, "/v1/auth/login", loginBody, testAccessToken)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{testAccessToken}, verifier.verified)
}

// send sends a request through the real routes, with token as the access token cookie
// unless it's empty.
func send(t *testing.T, h *Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	routes, err := h.Routes()
	require.NoError(t, err)

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: accessTokenCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec
}
