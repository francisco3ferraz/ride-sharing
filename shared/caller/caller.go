// Package caller carries the user a request acts for from the Edge to the services, in
// gRPC metadata. The Edge sets it from the verified access token. Services trust it
// because only the Edge can call them, and check the role themselves.
package caller

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// The metadata entries, one value each.
const (
	userIDKey = "user-id"
	roleKey   = "user-role"
)

// The roles an access token can carry, written as the token writes them.
const (
	RoleRider  = "rider"
	RoleDriver = "driver"
)

// User is the user a call acts for.
type User struct {
	ID   uuid.UUID
	Role string
}

// FromIncomingContext returns the user in the call's metadata. Its error is an INTERNAL
// status, for a handler to return as it is: the Edge always sends a logged-in user, so
// only a broken caller leaves them out.
func FromIncomingContext(ctx context.Context) (User, error) {
	ids := metadata.ValueFromIncomingContext(ctx, userIDKey)
	if len(ids) != 1 {
		return User{}, status.Error(codes.Internal, userIDKey+" metadata must have exactly one value")
	}
	// Parsed, not passed on as written: services key data by the ID, and an uppercase
	// or braced spelling of the same UUID would otherwise be another user. The nil UUID
	// is what an Edge that forgot to set the ID would send.
	id, err := uuid.Parse(ids[0])
	if err != nil || id == uuid.Nil {
		return User{}, status.Error(codes.Internal, userIDKey+" metadata must be a UUID")
	}

	roles := metadata.ValueFromIncomingContext(ctx, roleKey)
	if len(roles) != 1 || (roles[0] != RoleRider && roles[0] != RoleDriver) {
		return User{}, status.Error(codes.Internal, roleKey+" metadata must be one of "+RoleRider+" or "+RoleDriver)
	}
	return User{ID: id, Role: roles[0]}, nil
}
