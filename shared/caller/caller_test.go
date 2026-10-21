package caller

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const driverID = "5f0c3a7e-2b1d-4e8a-9c6f-1a2b3c4d5e6f"

func TestFromIncomingContext(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		want User
	}{
		{
			name: "driver",
			md:   metadata.Pairs(userIDKey, driverID, roleKey, RoleDriver),
			want: User{ID: uuid.MustParse(driverID), Role: RoleDriver},
		},
		{
			name: "rider",
			md:   metadata.Pairs(userIDKey, driverID, roleKey, RoleRider),
			want: User{ID: uuid.MustParse(driverID), Role: RoleRider},
		},
		{
			// The same user however the ID is spelled.
			name: "uppercase ID",
			md:   metadata.Pairs(userIDKey, "5F0C3A7E-2B1D-4E8A-9C6F-1A2B3C4D5E6F", roleKey, RoleDriver),
			want: User{ID: uuid.MustParse(driverID), Role: RoleDriver},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := FromIncomingContext(metadata.NewIncomingContext(t.Context(), tt.md))
			require.NoError(t, err)
			assert.Equal(t, tt.want, user)
		})
	}
}

func TestFromIncomingContextRejectsBadMetadata(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
	}{
		{name: "no metadata"},
		{name: "no ID", md: metadata.Pairs(roleKey, RoleDriver)},
		{name: "no role", md: metadata.Pairs(userIDKey, driverID)},
		{name: "two IDs", md: metadata.Pairs(userIDKey, driverID, userIDKey, uuid.NewString(), roleKey, RoleDriver)},
		{name: "two roles", md: metadata.Pairs(userIDKey, driverID, roleKey, RoleDriver, roleKey, RoleRider)},
		{name: "ID not a UUID", md: metadata.Pairs(userIDKey, "42", roleKey, RoleDriver)},
		{name: "nil UUID", md: metadata.Pairs(userIDKey, uuid.Nil.String(), roleKey, RoleDriver)},
		{name: "unknown role", md: metadata.Pairs(userIDKey, driverID, roleKey, "admin")},
		{name: "role in another case", md: metadata.Pairs(userIDKey, driverID, roleKey, "Driver")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			if tt.md != nil {
				ctx = metadata.NewIncomingContext(ctx, tt.md)
			}
			_, err := FromIncomingContext(ctx)
			assert.Equal(t, codes.Internal, status.Code(err))
		})
	}
}
