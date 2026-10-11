// Package grpcapi exposes the Identity service over gRPC. It validates requests and
// translates between protobuf messages and the account package.
package grpcapi

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/account"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
)

// AccountService is what Server needs from *account.Service, so tests can replace it.
type AccountService interface {
	Register(ctx context.Context, email, pw string, role account.Role) (account.User, error)
	Login(ctx context.Context, email, pw string) (account.User, error)
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

	user, err := s.accounts.Register(ctx, req.GetEmail(), req.GetPassword(), role)
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.RegisterResponse{User: userToProto(user)}, nil
}

func (s *Server) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.LoginResponse, error) {
	user, err := s.accounts.Login(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.LoginResponse{User: userToProto(user)}, nil
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
	case errors.Is(err, account.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, err.Error())
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
