// Package grpcapi exposes the Identity service over gRPC. It validates requests and
// translates between protobuf messages and the account and vehicle packages.
package grpcapi

import (
	"context"
	"errors"
	"net/netip"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/account"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/vehicle"
)

// AccountService is what Server needs from *account.Service, so tests can replace it.
type AccountService interface {
	Register(ctx context.Context, email, pw string, role account.Role) (account.User, account.Tokens, error)
	Login(ctx context.Context, email, pw string, clientIP netip.Addr) (account.User, account.Tokens, error)
	Refresh(ctx context.Context, token string) (account.Tokens, error)
	Logout(ctx context.Context, token string) error
	GetUser(ctx context.Context, userID uuid.UUID) (account.User, error)
	UpdateUser(ctx context.Context, userID uuid.UUID, name *string) (account.User, error)
	PutVehicle(ctx context.Context, userID uuid.UUID, v account.Vehicle) (account.Vehicle, bool, error)
	GetVehicle(ctx context.Context, userID uuid.UUID) (account.Vehicle, error)
	RequestPasswordReset(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, pw string) error
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
	// Without the address, the lockout can't count the attempt, so it doesn't go ahead.
	// INTERNAL, since only a broken caller leaves it out: the Edge always sends it.
	clientIP, ok := clientIPFrom(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, clientIPKey+" metadata is missing or isn't an IP address")
	}

	user, tokens, err := s.accounts.Login(ctx, req.GetEmail(), req.GetPassword(), clientIP)
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

func (s *Server) UpdateUser(ctx context.Context, req *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error) {
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be a UUID")
	}

	user, err := s.accounts.UpdateUser(ctx, userID, req.Name)
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.UpdateUserResponse{User: userToProto(user)}, nil
}

func (s *Server) GetVehicleCatalog(context.Context, *identityv1.GetVehicleCatalogRequest) (*identityv1.GetVehicleCatalogResponse, error) {
	resp := &identityv1.GetVehicleCatalogResponse{Colours: vehicle.Colours}
	for _, m := range vehicle.Makes {
		resp.Makes = append(resp.Makes, &identityv1.VehicleMake{Name: m.Name, Models: m.Models})
	}
	return resp, nil
}

func (s *Server) PutVehicle(ctx context.Context, req *identityv1.PutVehicleRequest) (*identityv1.PutVehicleResponse, error) {
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be a UUID")
	}

	v := req.GetVehicle()
	saved, created, err := s.accounts.PutVehicle(ctx, userID, account.Vehicle{
		Make: v.GetMake(), Model: v.GetModel(), Colour: v.GetColour(), Plate: v.GetPlate(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.PutVehicleResponse{Vehicle: vehicleToProto(saved), Created: created}, nil
}

func (s *Server) GetVehicle(ctx context.Context, req *identityv1.GetVehicleRequest) (*identityv1.GetVehicleResponse, error) {
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be a UUID")
	}

	v, err := s.accounts.GetVehicle(ctx, userID)
	if err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.GetVehicleResponse{Vehicle: vehicleToProto(v)}, nil
}

// An empty email isn't INVALID_ARGUMENT: it matches no user, so it succeeds like any
// unknown one.
func (s *Server) RequestPasswordReset(ctx context.Context, req *identityv1.RequestPasswordResetRequest) (*identityv1.RequestPasswordResetResponse, error) {
	if err := s.accounts.RequestPasswordReset(ctx, req.GetEmail()); err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.RequestPasswordResetResponse{}, nil
}

func (s *Server) ResetPassword(ctx context.Context, req *identityv1.ResetPasswordRequest) (*identityv1.ResetPasswordResponse, error) {
	if err := s.accounts.ResetPassword(ctx, req.GetToken(), req.GetPassword()); err != nil {
		return nil, toStatus(err)
	}
	return &identityv1.ResetPasswordResponse{}, nil
}

// clientIPKey is the metadata entry the Edge sends the client's address in.
const clientIPKey = "client-ip"

// clientIPFrom returns the client's address from the call's metadata. It's trusted
// because only the Edge can call Identity, which doesn't otherwise know who the client
// is.
func clientIPFrom(ctx context.Context) (netip.Addr, bool) {
	values := metadata.ValueFromIncomingContext(ctx, clientIPKey)
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(values[0])
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// toStatus maps errors to the codes in the proto's comments. A handler must never
// return a plain error, or a nil response with a nil error, which gRPC sends as OK.
func toStatus(err error) error {
	switch {
	// The Edge shows InvalidArgument messages to the user. These are written for them.
	case errors.Is(err, password.ErrInvalid), errors.Is(err, account.ErrInvalidResetToken),
		errors.Is(err, account.ErrInvalidName), errors.Is(err, vehicle.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, account.ErrEmailTaken), errors.Is(err, account.ErrPlateTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, account.ErrNotDriver):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, account.ErrInvalidCredentials), errors.Is(err, account.ErrInvalidRefreshToken):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, account.ErrUserNotFound), errors.Is(err, account.ErrNoVehicle):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, account.ErrTooManyLoginAttempts):
		return status.Error(codes.ResourceExhausted, err.Error())
	// Nothing was done, so the caller can try again shortly.
	case errors.Is(err, password.ErrBusy):
		return status.Error(codes.Unavailable, err.Error())
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
		Name:  user.Name,
	}
}

func vehicleToProto(v account.Vehicle) *identityv1.Vehicle {
	return &identityv1.Vehicle{Make: v.Make, Model: v.Model, Colour: v.Colour, Plate: v.Plate}
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
