package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/accesstoken"
)

// The access token's security scheme in openapi.yaml.
const cookieAuthScheme = "CookieAuth"

// The validator shows it to the client, after "security requirements failed: ".
var errNoAccessToken = errors.New("a valid access token is required")

// Unexported, so no other package can read or replace the claims.
type claimsKey struct{}

func withClaims(ctx context.Context, claims accesstoken.Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// claimsFrom returns the claims of the request's access token. ok is false when it had
// none, or one that failed verification.
func claimsFrom(ctx context.Context) (accesstoken.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(accesstoken.Claims)
	return claims, ok
}

// authenticate puts a valid access token's claims in the request's context, and
// otherwise passes the request on unchanged. It never rejects one: requireClaims does,
// for the routes the spec protects, so the spec stays the one list of them. It must
// wrap the validator, which runs requireClaims.
func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.Cookie fails only when there's no such cookie.
		cookie, err := r.Cookie(accessTokenCookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := h.verifier.Verify(r.Context(), cookie.Value)
		if err != nil {
			// Not logged: a token expiring every 15 minutes is routine.
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
	})
}

// requireClaims is the validator's AuthenticationFunc. kin-openapi calls it for every
// operation without `security: []`, before checking parameters or the body, so an
// anonymous caller gets a 401 rather than learning what a valid request looks like.
// ctx is the request's, where authenticate put the claims.
func requireClaims(ctx context.Context, input *openapi3filter.AuthenticationInput) error {
	// A scheme added to the spec later needs a check of its own, not a pass because a
	// cookie happened to verify.
	if input.SecuritySchemeName != cookieAuthScheme {
		return fmt.Errorf("unknown security scheme %q", input.SecuritySchemeName)
	}
	if _, ok := claimsFrom(ctx); !ok {
		return errNoAccessToken
	}
	return nil
}
