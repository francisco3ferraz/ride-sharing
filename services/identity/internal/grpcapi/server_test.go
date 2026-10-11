package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/account"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
)

var testID = uuid.MustParse("0199d3a2-7c41-7b1e-9f2a-3c5d8e6f1a2b")

// fakeAccounts fails with err when it's set. Otherwise it returns a user built from its
// arguments, so a test sees whether the role made it through both translations.
type fakeAccounts struct {
	err    error
	called bool
}

func (f *fakeAccounts) Register(_ context.Context, email, _ string, role account.Role) (account.User, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, f.err
	}
	return account.User{ID: testID, Email: email, Role: role}, nil
}

func (f *fakeAccounts) Login(_ context.Context, email, _ string) (account.User, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, f.err
	}
	return account.User{ID: testID, Email: email, Role: account.RoleDriver}, nil
}

func TestRegister(t *testing.T) {
	for _, role := range []identityv1.Role{identityv1.Role_ROLE_RIDER, identityv1.Role_ROLE_DRIVER} {
		t.Run(role.String(), func(t *testing.T) {
			srv := New(&fakeAccounts{})

			resp, err := srv.Register(context.Background(), &identityv1.RegisterRequest{
				Email:    "ana@example.pt",
				Password: "a long enough password",
				Role:     role,
			})

			require.NoError(t, err)
			assert.Equal(t, testID.String(), resp.GetUser().GetId())
			assert.Equal(t, "ana@example.pt", resp.GetUser().GetEmail())
			assert.Equal(t, role, resp.GetUser().GetRole())
		})
	}
}

func TestRegisterRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		req  *identityv1.RegisterRequest
	}{
		{
			name: "empty email",
			req:  &identityv1.RegisterRequest{Password: "a long enough password", Role: identityv1.Role_ROLE_RIDER},
		},
		{
			name: "unspecified role",
			req:  &identityv1.RegisterRequest{Email: "ana@example.pt", Password: "a long enough password"},
		},
		{
			name: "unknown role",
			req: &identityv1.RegisterRequest{
				Email:    "ana@example.pt",
				Password: "a long enough password",
				Role:     identityv1.Role(99),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &fakeAccounts{}
			srv := New(accounts)

			_, err := srv.Register(context.Background(), tt.req)

			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.False(t, accounts.called)
		})
	}
}

func TestLogin(t *testing.T) {
	srv := New(&fakeAccounts{})

	resp, err := srv.Login(context.Background(), &identityv1.LoginRequest{
		Email:    "ana@example.pt",
		Password: "a long enough password",
	})

	require.NoError(t, err)
	assert.Equal(t, testID.String(), resp.GetUser().GetId())
	assert.Equal(t, "ana@example.pt", resp.GetUser().GetEmail())
	assert.Equal(t, identityv1.Role_ROLE_DRIVER, resp.GetUser().GetRole())
}

// Both RPCs share the mapping, so each case runs through both.
func TestErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{
			name: "password breaks the rule",
			err:  fmt.Errorf("%w: must be at least 15 characters", password.ErrInvalid),
			want: codes.InvalidArgument,
		},
		{name: "email taken", err: account.ErrEmailTaken, want: codes.AlreadyExists},
		{name: "invalid credentials", err: account.ErrInvalidCredentials, want: codes.Unauthenticated},
		{
			name: "caller cancelled",
			err:  fmt.Errorf("wait for hashing slot: %w", context.Canceled),
			want: codes.Canceled,
		},
		{
			name: "caller's deadline passed",
			err:  fmt.Errorf("get user: %w", context.DeadlineExceeded),
			want: codes.DeadlineExceeded,
		},
		{name: "unexpected", err: errors.New("create user: connection refused"), want: codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(&fakeAccounts{err: tt.err})

			_, registerErr := srv.Register(context.Background(), &identityv1.RegisterRequest{
				Email:    "ana@example.pt",
				Password: "a long enough password",
				Role:     identityv1.Role_ROLE_RIDER,
			})
			_, loginErr := srv.Login(context.Background(), &identityv1.LoginRequest{
				Email:    "ana@example.pt",
				Password: "a long enough password",
			})

			for _, err := range []error{registerErr, loginErr} {
				assert.Equal(t, tt.want, status.Code(err))
				assert.Equal(t, tt.err.Error(), status.Convert(err).Message())
			}
		})
	}
}
