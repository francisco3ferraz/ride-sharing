// Package accesstoken verifies the access tokens Identity signs: JWTs signed with
// Ed25519 (EdDSA), checked against the public keys Identity publishes as a JWKS.
package accesstoken

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

const (
	issuer   = "identity"
	audience = "edge"

	// Bounds how long a key Identity stops publishing stays trusted.
	refreshInterval = 5 * time.Minute
	// A token signed with a key the cache doesn't hold triggers a fetch. The limit
	// stops made-up kids from flooding Identity.
	unknownKIDFetchInterval = time.Minute
	// Bounds both the wait for that limit and the fetch it allows, which run inside
	// the request.
	unknownKIDWaitMax = 2 * time.Second
	// Covers only the first fetch and the periodic ones.
	fetchTimeout = 2 * time.Second
	// Identity's and the Edge's clocks can differ by a little, and without this a token
	// used right after login would be rejected as issued in the future.
	clockSkew = 5 * time.Second
)

// Claims are an access token's claims. Role is "rider" or "driver".
type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}

// Validate runs after the parser's own checks, so it only sees tokens Identity signed.
func (c *Claims) Validate() error {
	if c.Subject == "" {
		return errors.New("missing subject")
	}
	if c.Role != "rider" && c.Role != "driver" {
		return fmt.Errorf("unknown role %q", c.Role)
	}
	return nil
}

type Verifier struct {
	keys   keyfunc.Keyfunc
	parser *jwt.Parser
}

// New fetches the JWKS at jwksURL and refreshes it in the background until ctx ends.
// It succeeds even when the first fetch fails, so the Edge starts while Identity is
// down; tokens are rejected until a fetch succeeds.
func New(ctx context.Context, jwksURL string) (*Verifier, error) {
	keys, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{jwksURL}, keyfunc.Override{
		RefreshInterval:           refreshInterval,
		RefreshUnknownKID:         rate.NewLimiter(rate.Every(unknownKIDFetchInterval), 1),
		RateLimitWaitMax:          unknownKIDWaitMax,
		HTTPTimeout:               fetchTimeout,
		NoErrorReturnFirstHTTPReq: new(true),
	})
	if err != nil {
		return nil, fmt.Errorf("create jwks cache: %w", err)
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkew),
	)
	return &Verifier{keys: keys, parser: parser}, nil
}

// Verify checks the token's signature and claims, and returns its claims. ctx should be
// the request's, so a client that disconnects ends the fetch an unknown kid triggers.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	var claims Claims
	if _, err := v.parser.ParseWithClaims(token, &claims, v.lookupKey(ctx)); err != nil {
		return Claims{}, fmt.Errorf("verify access token: %w", err)
	}
	return claims, nil
}

// lookupKey requires a kid, which Identity always sets. Without one, keyfunc would try
// the token against every key in the set.
func (v *Verifier) lookupKey(ctx context.Context) jwt.Keyfunc {
	lookup := v.keys.KeyfuncCtx(ctx)
	return func(token *jwt.Token) (any, error) {
		if kid, _ := token.Header["kid"].(string); kid == "" {
			return nil, errors.New("token has no kid")
		}
		return lookup(token)
	}
}
