package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/api"
)

// Identity hashes a password on every call and limits how many hashes run at once, so
// in a burst of logins requests wait their turn. Revisit after measuring one.
const identityTimeout = 5 * time.Second

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
	return api.RegisterUser201JSONResponse(user), nil
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
	return api.Login200JSONResponse(user), nil
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
