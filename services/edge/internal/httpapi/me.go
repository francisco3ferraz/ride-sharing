package httpapi

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// One indexed query, with no hashing slot to wait for.
const userTimeout = time.Second

// For Identity's NotFound: the token outlived its user. A 401 sends the web app to
// refresh, then to log in.
var errUserGone = status.Error(codes.Unauthenticated, "the access token's user no longer exists")

// GetMe answers with the user the access token names. The ID comes from the verified
// token, never from the request, so a caller can only ever read their own account.
func (h *Handler) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	// The validator lets a request through only with claims, so none here means
	// authenticate isn't wired in: a 500, not the client's fault.
	claims, ok := claimsFrom(ctx)
	if !ok {
		return nil, errors.New("no access token claims in context")
	}

	ctx, cancel := context.WithTimeout(ctx, userTimeout)
	defer cancel()

	resp, err := h.identity.GetUser(ctx, &identityv1.GetUserRequest{UserId: claims.Subject})
	if status.Code(err) == codes.NotFound {
		return nil, errUserGone
	}
	if err != nil {
		return nil, err
	}
	user, err := userFromProto(resp.GetUser())
	if err != nil {
		return nil, err
	}
	return api.GetMe200JSONResponse(user), nil
}

// UpdateMe changes the fields the request sets, on the access token's user.
func (h *Handler) UpdateMe(ctx context.Context, req api.UpdateMeRequestObject) (api.UpdateMeResponseObject, error) {
	claims, ok := claimsFrom(ctx)
	if !ok {
		return nil, errors.New("no access token claims in context")
	}

	ctx, cancel := context.WithTimeout(ctx, userTimeout)
	defer cancel()

	resp, err := h.identity.UpdateUser(ctx, &identityv1.UpdateUserRequest{UserId: claims.Subject, Name: req.Body.Name})
	if status.Code(err) == codes.NotFound {
		return nil, errUserGone
	}
	if err != nil {
		return nil, err
	}
	user, err := userFromProto(resp.GetUser())
	if err != nil {
		return nil, err
	}
	return api.UpdateMe200JSONResponse(user), nil
}
