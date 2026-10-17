package httpapi

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

const (
	testUserID       = "0199d3a2-7c41-7b1e-9f2a-3c5d8e6f1a2b"
	testAccessToken  = "header.payload.signature"
	testRefreshToken = "opaque-refresh-token"
)

// fakeIdentity stands in for the Identity service, like fakePricing.
type fakeIdentity struct {
	user        *identityv1.User
	tokens      *identityv1.Tokens
	err         error
	gotRegister *identityv1.RegisterRequest
	gotLogin    *identityv1.LoginRequest
	gotGetUser  *identityv1.GetUserRequest
}

func (f *fakeIdentity) Register(_ context.Context, req *identityv1.RegisterRequest, _ ...grpc.CallOption) (*identityv1.RegisterResponse, error) {
	f.gotRegister = req
	if f.err != nil {
		return nil, f.err
	}
	return &identityv1.RegisterResponse{User: f.user, Tokens: f.tokens}, nil
}

func (f *fakeIdentity) Login(_ context.Context, req *identityv1.LoginRequest, _ ...grpc.CallOption) (*identityv1.LoginResponse, error) {
	f.gotLogin = req
	if f.err != nil {
		return nil, f.err
	}
	return &identityv1.LoginResponse{User: f.user, Tokens: f.tokens}, nil
}

func (f *fakeIdentity) GetUser(_ context.Context, req *identityv1.GetUserRequest, _ ...grpc.CallOption) (*identityv1.GetUserResponse, error) {
	f.gotGetUser = req
	if f.err != nil {
		return nil, f.err
	}
	return &identityv1.GetUserResponse{User: f.user}, nil
}

func testTokens() *identityv1.Tokens {
	now := time.Now()
	return &identityv1.Tokens{
		AccessToken:  &identityv1.AccessToken{Token: testAccessToken, ExpiresAt: timestamppb.New(now.Add(15 * time.Minute))},
		RefreshToken: &identityv1.RefreshToken{Token: testRefreshToken, ExpiresAt: timestamppb.New(now.Add(7 * 24 * time.Hour))},
	}
}

func brokenTokens(breakIt func(*identityv1.Tokens)) *identityv1.Tokens {
	tokens := testTokens()
	breakIt(tokens)
	return tokens
}

func TestRegisterUser(t *testing.T) {
	tests := []struct {
		role      string
		protoRole identityv1.Role
	}{
		{role: "rider", protoRole: identityv1.Role_ROLE_RIDER},
		{role: "driver", protoRole: identityv1.Role_ROLE_DRIVER},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			identity := &fakeIdentity{
				user:   &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: tt.protoRole},
				tokens: testTokens(),
			}
			rec := post(t, identity, "/v1/auth/register",
				`{"email": "ana@example.pt", "password": "a long enough password", "role": "`+tt.role+`"}`)

			assert.Equal(t, http.StatusCreated, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"id": "`+testUserID+`", "email": "ana@example.pt", "role": "`+tt.role+`"}`, rec.Body.String())
			assertSessionCookies(t, rec)

			require.NotNil(t, identity.gotRegister)
			assert.Equal(t, "ana@example.pt", identity.gotRegister.GetEmail())
			assert.Equal(t, "a long enough password", identity.gotRegister.GetPassword())
			assert.Equal(t, tt.protoRole, identity.gotRegister.GetRole())
		})
	}
}

func TestRegisterUserRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "no body", body: "", want: http.StatusBadRequest},
		{
			name: "missing role",
			body: `{"email": "ana@example.pt", "password": "a long enough password"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "unknown role",
			body: `{"email": "ana@example.pt", "password": "a long enough password", "role": "admin"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "malformed email",
			body: `{"email": "ana", "password": "a long enough password", "role": "rider"}`,
			want: http.StatusBadRequest,
		},
		// Only the registered email format rejects this: the generated Email type would
		// accept it and keep just the address.
		{
			name: "email with a display name",
			body: `{"email": "Ana <ana@example.pt>", "password": "a long enough password", "role": "rider"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "body over the limit",
			body: `{"email": "` + strings.Repeat("a", maxBodyBytes) + `@example.pt", "password": "x", "role": "rider"}`,
			want: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &fakeIdentity{}
			rec := post(t, identity, "/v1/auth/register", tt.body)

			assert.Equal(t, tt.want, rec.Code)
			assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			assert.Nil(t, identity.gotRegister, "identity should not be called")
		})
	}
}

func TestLogin(t *testing.T) {
	identity := &fakeIdentity{
		user:   &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER},
		tokens: testTokens(),
	}
	rec := post(t, identity, "/v1/auth/login", `{"email": "Ana@example.pt", "password": "a long enough password"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id": "`+testUserID+`", "email": "ana@example.pt", "role": "rider"}`, rec.Body.String())
	assertSessionCookies(t, rec)

	require.NotNil(t, identity.gotLogin)
	assert.Equal(t, "Ana@example.pt", identity.gotLogin.GetEmail())
	assert.Equal(t, "a long enough password", identity.gotLogin.GetPassword())
}

func TestAuthMapsIdentityErrors(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   string
		err    error
		want   api.Problem
	}{
		{
			name:   "taken email",
			target: "/v1/auth/register",
			body:   `{"email": "ana@example.pt", "password": "a long enough password", "role": "rider"}`,
			err:    status.Error(codes.AlreadyExists, "email is already registered"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Conflict",
				Status: http.StatusConflict,
				Detail: new("email is already registered"),
			},
		},
		{
			name:   "password breaks the rule",
			target: "/v1/auth/register",
			body:   `{"email": "ana@example.pt", "password": "too short", "role": "rider"}`,
			err:    status.Error(codes.InvalidArgument, "invalid password: must be at least 15 characters"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Bad Request",
				Status: http.StatusBadRequest,
				Detail: new("invalid password: must be at least 15 characters"),
			},
		},
		{
			name:   "wrong email or password",
			target: "/v1/auth/login",
			body:   `{"email": "ana@example.pt", "password": "not the right password"}`,
			err:    status.Error(codes.Unauthenticated, "invalid email or password"),
			want: api.Problem{
				Type:   "about:blank",
				Title:  "Unauthorized",
				Status: http.StatusUnauthorized,
				Detail: new("invalid email or password"),
			},
		},
		{
			name:   "internal failure",
			target: "/v1/auth/login",
			body:   `{"email": "ana@example.pt", "password": "a long enough password"}`,
			err:    status.Error(codes.Internal, "get user: connection refused"),
			want:   api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(t, &fakeIdentity{err: tt.err}, tt.target, tt.body)

			assertProblem(t, rec, tt.want)
			assert.Empty(t, rec.Header().Values("Set-Cookie"))
		})
	}
}

func TestAuthRejectsBrokenIdentityResponses(t *testing.T) {
	rider := &identityv1.User{Id: testUserID, Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER}
	tests := []struct {
		name   string
		user   *identityv1.User
		tokens *identityv1.Tokens
	}{
		{
			name:   "id isn't a uuid",
			user:   &identityv1.User{Id: "42", Email: "ana@example.pt", Role: identityv1.Role_ROLE_RIDER},
			tokens: testTokens(),
		},
		{name: "role unspecified", user: &identityv1.User{Id: testUserID, Email: "ana@example.pt"}, tokens: testTokens()},
		{name: "no tokens", user: rider},
		{
			name:   "empty access token",
			user:   rider,
			tokens: brokenTokens(func(tokens *identityv1.Tokens) { tokens.AccessToken.Token = "" }),
		},
		{
			name:   "access token without expiry",
			user:   rider,
			tokens: brokenTokens(func(tokens *identityv1.Tokens) { tokens.AccessToken.ExpiresAt = nil }),
		},
		{
			name:   "no refresh token",
			user:   rider,
			tokens: brokenTokens(func(tokens *identityv1.Tokens) { tokens.RefreshToken = nil }),
		},
		{
			name: "refresh token already expired",
			user: rider,
			tokens: brokenTokens(func(tokens *identityv1.Tokens) {
				tokens.RefreshToken.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(t, &fakeIdentity{user: tt.user, tokens: tt.tokens}, "/v1/auth/login",
				`{"email": "ana@example.pt", "password": "a long enough password"}`)

			assertProblem(t, rec, api.Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError})
			assert.Empty(t, rec.Header().Values("Set-Cookie"))
		})
	}
}

// Go doesn't enforce the __Host- rules, but the browser silently drops a cookie that
// breaks them, so every attribute is checked.
func assertSessionCookies(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := []struct {
		name, value, path string
		sameSite          http.SameSite
		lifetime          time.Duration
	}{
		{name: "__Host-access_token", value: testAccessToken, path: "/", sameSite: http.SameSiteLaxMode, lifetime: 15 * time.Minute},
		{
			name:     "__Secure-refresh_token",
			value:    testRefreshToken,
			path:     "/v1/auth",
			sameSite: http.SameSiteStrictMode,
			lifetime: 7 * 24 * time.Hour,
		},
	}

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, len(want))
	for i, w := range want {
		got := cookies[i]
		assert.Equal(t, w.name, got.Name)
		assert.Equal(t, w.value, got.Value)
		assert.Equal(t, w.path, got.Path)
		assert.Empty(t, got.Domain)
		assert.Equal(t, w.sameSite, got.SameSite)
		assert.True(t, got.Secure, "Secure")
		assert.True(t, got.HttpOnly, "HttpOnly")
		// The handler counts from its own now, a moment after testTokens', and drops
		// the fraction of a second.
		assert.InDelta(t, w.lifetime.Seconds(), got.MaxAge, 2)
	}
}

// post sends a JSON body through the real routes, so tests cover the body limit and
// the validator too.
func post(t *testing.T, identity identityv1.IdentityServiceClient, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postWithHeader(t, identity, target, body, nil)
}

func postWithHeader(t *testing.T, identity identityv1.IdentityServiceClient, target, body string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	routes, err := NewHandler(nil, identity, nil).Routes()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	maps.Copy(req.Header, header)
	// Without it the validator answers 400 before any handler runs.
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec
}
