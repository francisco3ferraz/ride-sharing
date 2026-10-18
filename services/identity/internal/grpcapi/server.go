// Package grpcapi exposes the Identity service over gRPC. It validates requests and
// translates between protobuf messages and the account package.
package grpcapi

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/account"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
)

// AccountService is what Server needs from *account.Service, so tests can replace it.
type AccountService interface {
	Register(ctx context.Context, email, pw string, role account.Role) (account.User, account.Tokens, error)
	Login(ctx context.Context, email, pw string) (account.User, account.Tokens, error)
	Refresh(ctx context.Context, token string) (account.Tokens, error)
	Logout(ctx context.Context, token string) error
	GetUser(ctx context.Context, userID uuid.UUID) (account.User, error)
}

// Server implements identityv1.IdentityServiceServer.
type Server struct {
	identityv1.UnimplementedIdentityServiceServer
	accounts AccountService
}

func New(accounts AccountService) *Server {
	return &Server{accounts: accounts}
}

func (s *Server) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.RegisterResponse, error) {
	// The Edge checks the email's format. This stops direct callers storing an empty one,
	// which the users table would accept.
	if req.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	role, ok := roleFromProto(req.GetRole())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "role must be either rider or driver")
	}

	user, tokens, err := s.accounts.Register(ctx, req.GetEmail(), req.GetPassword(), role)
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.RegisterResponse{User: userToProto(user), Tokens: tokensToProto(tokens)}, nil
}

func (s *Server) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.LoginResponse, error) {
	user, tokens, err := s.accounts.Login(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.LoginResponse{User: userToProto(user), Tokens: tokensToProto(tokens)}, nil
}

// An empty token isn't INVALID_ARGUMENT: the Edge sends one when the cookie is missing,
// and needs UNAUTHENTICATED, which sends the web app to log in. It matches no stored
// token, so it fails like any unknown one.
func (s *Server) Refresh(ctx context.Context, req *identityv1.RefreshRequest) (*identityv1.RefreshResponse, error) {
	tokens, err := s.accounts.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.RefreshResponse{Tokens: tokensToProto(tokens)}, nil
}

func (s *Server) Logout(ctx context.Context, req *identityv1.LogoutRequest) (*identityv1.LogoutResponse, error) {
	if err := s.accounts.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.LogoutResponse{}, nil
}

func (s *Server) GetUser(ctx context.Context, req *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
	// Not uuid's own error, which describes its parser ("invalid UUID length: 1"), and
	// INVALID_ARGUMENT messages are shown to the caller.
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be a UUID")
	}

	user, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.GetUserResponse{User: userToProto(user)}, nil
}

// toStatus maps errors to the codes in the proto's comments. A handler must never
// return a plain error, or a nil response with a nil error, which gRPC sends as OK.
func toStatus(err error) error {
	switch {
	// The Edge shows InvalidArgument messages to the user. This one is written for them.
	case errors.Is(err, password.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, account.ErrEmailTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, account.ErrInvalidCredentials), errors.Is(err, account.ErrInvalidRefreshToken):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, account.ErrUserNotFound):
		return status.Error(codes.NotFound, err.Error())
	// The caller gave up, for example while waiting for a hashing slot. Not our fault.
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	// The Edge logs the message and answers with a generic 500, so the detail reaches
	// its log and this RPC's span without leaving the cluster.
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// Proto3 enums are open: a caller can send any number, so anything but the two known
// roles is rejected, not just ROLE_UNSPECIFIED.
func roleFromProto(role identityv1.Role) (account.Role, bool) {
	switch role {
	case identityv1.Role_ROLE_RIDER:
		return account.RoleRider, true
	case identityv1.Role_ROLE_DRIVER:
		return account.RoleDriver, true
	default:
		return "", false
	}
}

func roleToProto(role account.Role) identityv1.Role {
	switch role {
	case account.RoleRider:
		return identityv1.Role_ROLE_RIDER
	case account.RoleDriver:
		return identityv1.Role_ROLE_DRIVER
	default:
		return identityv1.Role_ROLE_UNSPECIFIED
	}
}

func userToProto(user account.User) *identityv1.User {
	return &identityv1.User{
		Id:    user.ID.String(),
		Email: user.Email,
		Role:  roleToProto(user.Role),
	}
}

func tokensToProto(tokens account.Tokens) *identityv1.Tokens {
	return &identityv1.Tokens{
		AccessToken: &identityv1.AccessToken{
			Token:     tokens.Access.Value,
			ExpiresAt: timestamppb.New(tokens.Access.ExpiresAt),
		},
		RefreshToken: &identityv1.RefreshToken{
			Token:     tokens.Refresh.Value,
			ExpiresAt: timestamppb.New(tokens.Refresh.ExpiresAt),
		},
	}
}
