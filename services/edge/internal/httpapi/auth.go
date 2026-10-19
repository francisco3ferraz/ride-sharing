package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// Register, login and password reset hash a password. Identity limits how many hashes
// run at once and how many wait, and turns the rest away, so in a burst of logins
// requests wait their turn only briefly. Its queue must stay well inside this, or every
// slot hashes for a request about to time out. Refresh hashes nothing but gets the same
// room: one that times out after Identity commits leaves the browser holding a used
// token, so its next refresh ends the session.
const identityTimeout = 2 * time.Second

func (h *Handler) RegisterUser(ctx context.Context, req api.RegisterUserRequestObject) (api.RegisterUserResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	resp, err := h.identity.Register(ctx, &identityv1.RegisterRequest{
		Email:    string(req.Body.Email),
		Password: req.Body.Password,
		Role:     roleToProto(req.Body.Role),
	})
	if err != nil {
		return nil, err
	}
	user, err := userFromProto(resp.GetUser())
	if err != nil {
		return nil, err
	}
	cookies, err := sessionCookies(resp.GetTokens())
	if err != nil {
		return nil, err
	}
	return registerUserResponse{cookies: cookies, next: api.RegisterUser201JSONResponse{Body: user}}, nil
}

func (h *Handler) Login(ctx context.Context, req api.LoginRequestObject) (api.LoginResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	resp, err := h.identity.Login(ctx, &identityv1.LoginRequest{
		Email:    req.Body.Email,
		Password: req.Body.Password,
	})
	if err != nil {
		return nil, err
	}
	user, err := userFromProto(resp.GetUser())
	if err != nil {
		return nil, err
	}
	cookies, err := sessionCookies(resp.GetTokens())
	if err != nil {
		return nil, err
	}
	return loginResponse{cookies: cookies, next: api.Login200JSONResponse{Body: user}}, nil
}

func (h *Handler) Refresh(ctx context.Context, req api.RefreshRequestObject) (api.RefreshResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	resp, err := h.identity.Refresh(ctx, &identityv1.RefreshRequest{
		RefreshToken: refreshTokenValue(req.Params.RefreshToken),
	})
	if err != nil {
		return nil, err
	}
	cookies, err := sessionCookies(resp.GetTokens())
	if err != nil {
		return nil, err
	}
	return refreshResponse{cookies: cookies}, nil
}

func (h *Handler) Logout(ctx context.Context, req api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	_, err := h.identity.Logout(ctx, &identityv1.LogoutRequest{
		RefreshToken: refreshTokenValue(req.Params.RefreshToken),
	})
	if err != nil {
		// The cookies stay, so the user can retry: clearing them would make the browser
		// forget a refresh token that still works.
		return nil, err
	}
	return logoutResponse{cookies: expiredSessionCookies()}, nil
}

func (h *Handler) RequestPasswordReset(ctx context.Context, req api.RequestPasswordResetRequestObject) (api.RequestPasswordResetResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	_, err := h.identity.RequestPasswordReset(ctx, &identityv1.RequestPasswordResetRequest{Email: req.Body.Email})
	if err != nil {
		return nil, err
	}
	return api.RequestPasswordReset202Response{}, nil
}

// The user's sessions end, but this browser's cookies stay: its access token works until
// it expires, like every other access token already issued, and its next refresh fails.
func (h *Handler) ResetPassword(ctx context.Context, req api.ResetPasswordRequestObject) (api.ResetPasswordResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()

	_, err := h.identity.ResetPassword(ctx, &identityv1.ResetPasswordRequest{
		Token:    req.Body.Token,
		Password: req.Body.Password,
	})
	if err != nil {
		return nil, err
	}
	return api.ResetPassword204Response{}, nil
}

// Handlers never get the ResponseWriter, so the response sets the cookies. The
// generated response isn't embedded: its Visit method would be promoted, so a misnamed
// method here would still compile and set no cookies.
type registerUserResponse struct {
	cookies []*http.Cookie
	next    api.RegisterUser201JSONResponse
}

func (r registerUserResponse) VisitRegisterUserResponse(w http.ResponseWriter) error {
	setCookies(w, r.cookies)
	return r.next.VisitRegisterUserResponse(w)
}

type loginResponse struct {
	cookies []*http.Cookie
	next    api.Login200JSONResponse
}

func (r loginResponse) VisitLoginResponse(w http.ResponseWriter) error {
	setCookies(w, r.cookies)
	return r.next.VisitLoginResponse(w)
}

type refreshResponse struct {
	cookies []*http.Cookie
	next    api.Refresh204Response
}

func (r refreshResponse) VisitRefreshResponse(w http.ResponseWriter) error {
	setCookies(w, r.cookies)
	return r.next.VisitRefreshResponse(w)
}

type logoutResponse struct {
	cookies []*http.Cookie
	next    api.Logout204Response
}

func (r logoutResponse) VisitLogoutResponse(w http.ResponseWriter) error {
	setCookies(w, r.cookies)
	return r.next.VisitLogoutResponse(w)
}

// The validator lets only rider and driver through. Anything else would reach
// Identity as ROLE_UNSPECIFIED, which it rejects.
func roleToProto(role api.Role) identityv1.Role {
	switch role {
	case api.Rider:
		return identityv1.Role_ROLE_RIDER
	case api.Driver:
		return identityv1.Role_ROLE_DRIVER
	default:
		return identityv1.Role_ROLE_UNSPECIFIED
	}
}

// userFromProto fails only if Identity breaks its contract, which becomes a 500.
func userFromProto(user *identityv1.User) (api.User, error) {
	id, err := uuid.Parse(user.GetId())
	if err != nil {
		return api.User{}, fmt.Errorf("parse user id: %w", err)
	}
	role, err := roleFromProto(user.GetRole())
	if err != nil {
		return api.User{}, err
	}
	return api.User{Id: id, Email: user.GetEmail(), Role: role}, nil
}

func roleFromProto(role identityv1.Role) (api.Role, error) {
	switch role {
	case identityv1.Role_ROLE_RIDER:
		return api.Rider, nil
	case identityv1.Role_ROLE_DRIVER:
		return api.Driver, nil
	default:
		return "", fmt.Errorf("unexpected role %s", role)
	}
}
