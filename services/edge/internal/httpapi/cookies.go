package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
)

// The session cookies of ADR 0002. Protected routes will read the access token, and
// refresh and logout the refresh token.
const (
	// The __Host- prefix makes the browser require Secure and Path=/ and refuse a
	// Domain, so a sibling host can't plant or overwrite the cookie.
	accessTokenCookie = "__Host-access_token"
	// __Host- would need Path=/. __Secure- only makes the browser refuse the cookie
	// from a plain-HTTP response; a sibling host can still set it.
	refreshTokenCookie = "__Secure-refresh_token"
	// Refresh and logout must live under it, or the refresh cookie won't reach them.
	refreshTokenPath = "/v1/auth"
)

// issuedToken is what AccessToken and RefreshToken have in common.
type issuedToken interface {
	GetToken() string
	GetExpiresAt() *timestamppb.Timestamp
}

// sessionCookies turns the tokens Identity issued into the two cookies. Each lasts as
// long as its token, so lifetimes stay Identity's to set. It fails only if Identity
// breaks its contract, which becomes a 500.
func sessionCookies(tokens *identityv1.Tokens) ([]*http.Cookie, error) {
	access, err := tokenCookie(accessTokenCookie, "/", http.SameSiteLaxMode, tokens.GetAccessToken())
	if err != nil {
		return nil, fmt.Errorf("build access token cookie: %w", err)
	}
	refresh, err := tokenCookie(refreshTokenCookie, refreshTokenPath, http.SameSiteStrictMode, tokens.GetRefreshToken())
	if err != nil {
		return nil, fmt.Errorf("build refresh token cookie: %w", err)
	}
	return []*http.Cookie{access, refresh}, nil
}

func tokenCookie(name, path string, sameSite http.SameSite, token issuedToken) (*http.Cookie, error) {
	if token.GetToken() == "" {
		return nil, errors.New("empty token")
	}
	if err := token.GetExpiresAt().CheckValid(); err != nil {
		return nil, fmt.Errorf("check expiry: %w", err)
	}
	// http.Cookie reads MaxAge 0 as a session cookie and a negative one as "delete
	// now", so neither can stand for a token that's about to expire.
	maxAge := int(time.Until(token.GetExpiresAt().AsTime()).Seconds())
	if maxAge <= 0 {
		return nil, errors.New("token already expired")
	}
	return &http.Cookie{
		Name:  name,
		Value: token.GetToken(),
		Path:  path,
		// Counts from when the browser gets the cookie, so unlike Expires it doesn't
		// depend on the browser's clock.
		MaxAge: maxAge,
		// Always: Traefik terminates TLS, so the Edge itself only sees plain HTTP.
		Secure:   true,
		HttpOnly: true,
		SameSite: sameSite,
	}, nil
}

func setCookies(w http.ResponseWriter, cookies []*http.Cookie) {
	for _, cookie := range cookies {
		http.SetCookie(w, cookie)
	}
}
