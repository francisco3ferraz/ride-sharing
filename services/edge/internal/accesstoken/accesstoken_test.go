package accesstoken

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	key := generateKey(t)
	server := newJWKSServer(t)
	server.publish(t, "key-1", key)
	userID := uuid.NewString()

	claims, err := newVerifier(t, server.URL).Verify(t.Context(), sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) {
		c.Subject = userID
		c.Role = "driver"
	}))

	require.NoError(t, err)
	assert.Equal(t, userID, claims.Subject)
	assert.Equal(t, "driver", claims.Role)
}

func TestVerifyRejects(t *testing.T) {
	key := generateKey(t)
	otherKey := generateKey(t)
	server := newJWKSServer(t)
	server.publish(t, "key-1", key)
	verifier := newVerifier(t, server.URL)

	tests := []struct {
		name    string
		token   string
		wantErr error
	}{
		{
			name:    "another issuer",
			token:   sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) { c.Issuer = "someone-else" }),
			wantErr: jwt.ErrTokenInvalidIssuer,
		},
		{
			name:    "another audience",
			token:   sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) { c.Audience = jwt.ClaimStrings{"pricing"} }),
			wantErr: jwt.ErrTokenInvalidAudience,
		},
		{
			name:    "no expiry",
			token:   sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) { c.ExpiresAt = nil }),
			wantErr: jwt.ErrTokenRequiredClaimMissing,
		},
		{
			name: "expired",
			token: sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) {
				c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
			}),
			wantErr: jwt.ErrTokenExpired,
		},
		{
			name: "issued in the future",
			token: sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) {
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
			}),
			wantErr: jwt.ErrTokenUsedBeforeIssued,
		},
		{
			name:    "no subject",
			token:   sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) { c.Subject = "" }),
			wantErr: jwt.ErrTokenInvalidClaims,
		},
		{
			name:    "an unknown role",
			token:   sign(t, key, "key-1", func(_ *jwt.Token, c *Claims) { c.Role = "admin" }),
			wantErr: jwt.ErrTokenInvalidClaims,
		},
		{
			// The parser picks the algorithm from the header, which whoever made the
			// token controls.
			name:    "another algorithm",
			token:   sign(t, key, "key-1", func(tok *jwt.Token, _ *Claims) { tok.Header["alg"] = "HS256" }),
			wantErr: jwt.ErrTokenSignatureInvalid,
		},
		{
			name:    "no kid",
			token:   sign(t, key, "key-1", func(tok *jwt.Token, _ *Claims) { delete(tok.Header, "kid") }),
			wantErr: jwt.ErrTokenUnverifiable,
		},
		{
			name:    "a key that isn't published",
			token:   sign(t, otherKey, "key-2", nil),
			wantErr: jwt.ErrTokenUnverifiable,
		},
		{
			name:    "another key under a published kid",
			token:   sign(t, otherKey, "key-1", nil),
			wantErr: jwt.ErrTokenSignatureInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifier.Verify(t.Context(), tt.token)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// A new key works as soon as a token signed with it arrives, not at the next refresh.
func TestVerifyFetchesAnUnknownKey(t *testing.T) {
	oldKey := generateKey(t)
	newKey := generateKey(t)
	server := newJWKSServer(t)
	server.publish(t, "old", oldKey)
	verifier := newVerifier(t, server.URL)

	server.publish(t, "new", newKey)
	_, err := verifier.Verify(t.Context(), sign(t, newKey, "new", nil))

	assert.NoError(t, err)
}

func TestVerifyAfterIdentityWasDownAtStart(t *testing.T) {
	key := generateKey(t)
	server := newJWKSServer(t)
	verifier := newVerifier(t, server.URL)

	server.publish(t, "key-1", key)
	_, err := verifier.Verify(t.Context(), sign(t, key, "key-1", nil))

	assert.NoError(t, err)
}

func TestVerifyLimitsFetchesForUnknownKeys(t *testing.T) {
	key := generateKey(t)
	server := newJWKSServer(t)
	server.publish(t, "key-1", key)
	verifier := newVerifier(t, server.URL)
	require.EqualValues(t, 1, server.fetches.Load())

	for _, kid := range []string{"made-up-1", "made-up-2"} {
		_, err := verifier.Verify(t.Context(), sign(t, key, kid, nil))
		assert.ErrorIs(t, err, jwt.ErrTokenUnverifiable)
	}

	// Only the first made-up kid triggered a fetch. The second failed at once, without
	// waiting out the limit.
	assert.EqualValues(t, 2, server.fetches.Load())
}

// jwksServer stands in for Identity's JWKS endpoint. It answers 503, as if Identity
// were down, until a key is published.
type jwksServer struct {
	*httptest.Server
	jwks    atomic.Pointer[[]byte]
	fetches atomic.Int32
}

func newJWKSServer(t *testing.T) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.fetches.Add(1)
		jwks := s.jwks.Load()
		if jwks == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(*jwks)
	}))
	t.Cleanup(s.Close)
	return s
}

// publish replaces the served set with one holding only key's public half.
func (s *jwksServer) publish(t *testing.T, kid string, key ed25519.PrivateKey) {
	t.Helper()
	jwks, err := json.Marshal(map[string][]map[string]string{"keys": {{
		"kty": "OKP",
		"crv": "Ed25519",
		"x":   base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
		"kid": kid,
		"alg": "EdDSA",
		"use": "sig",
	}}})
	require.NoError(t, err)
	s.jwks.Store(&jwks)
}

// newVerifier builds the Verifier with the test's context, so its background refresh
// stops when the test ends.
func newVerifier(t *testing.T, jwksURL string) *Verifier {
	t.Helper()
	v, err := New(t.Context(), jwksURL)
	require.NoError(t, err)
	return v
}

func generateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return key
}

// sign returns a token with valid claims, signed with key under kid. edit, when not
// nil, changes the token or its claims before it's signed.
func sign(t *testing.T, key ed25519.PrivateKey, kid string, edit func(*jwt.Token, *Claims)) string {
	t.Helper()
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "identity",
			Subject:   uuid.NewString(),
			Audience:  jwt.ClaimStrings{"edge"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
		Role: "rider",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, &claims)
	token.Header["kid"] = kid
	if edit != nil {
		edit(token, &claims)
	}
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}
