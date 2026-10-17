package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

var testTokens = account.Tokens{
	Access:  account.Token{Value: "access-token", ExpiresAt: time.Date(2026, 10, 13, 9, 45, 0, 0, time.UTC)},
	Refresh: account.Token{Value: "refresh-token", ExpiresAt: time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)},
}

// fakeAccounts fails with err when it's set. Otherwise it returns testTokens and a user
// built from its arguments, so a test sees whether the role made it through both
// translations.
type fakeAccounts struct {
	err    error
	called bool
}

func (f *fakeAccounts) Register(_ context.Context, email, _ string, role account.Role) (account.User, account.Tokens, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, account.Tokens{}, f.err
	}
	return account.User{ID: testID, Email: email, Role: role}, testTokens, nil
}

func (f *fakeAccounts) Login(_ context.Context, email, _ string) (account.User, account.Tokens, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, account.Tokens{}, f.err
	}
	return account.User{ID: testID, Email: email, Role: account.RoleDriver}, testTokens, nil
}

func (f *fakeAccounts) GetUser(_ context.Context, userID uuid.UUID) (account.User, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, f.err
	}
	return account.User{ID: userID, Email: "ana@example.pt", Role: account.RoleRider}, nil
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
			assertTestTokens(t, resp.GetTokens())
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
	assertTestTokens(t, resp.GetTokens())
}

func TestGetUser(t *testing.T) {
	srv := New(&fakeAccounts{})

	resp, err := srv.GetUser(context.Background(), &identityv1.GetUserRequest{UserId: testID.String()})

	require.NoError(t, err)
	assert.Equal(t, testID.String(), resp.GetUser().GetId())
	assert.Equal(t, "ana@example.pt", resp.GetUser().GetEmail())
	assert.Equal(t, identityv1.Role_ROLE_RIDER, resp.GetUser().GetRole())
}

func TestGetUserRejectsMalformedIDs(t *testing.T) {
	tests := []struct {
		name   string
		userID string
	}{
		{name: "empty", userID: ""},
		{name: "not a uuid", userID: "x"},
		{name: "cut short", userID: "0199d3a2-7c41-7b1e-9f2a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &fakeAccounts{}
			srv := New(accounts)

			_, err := srv.GetUser(context.Background(), &identityv1.GetUserRequest{UserId: tt.userID})

			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Equal(t, "user_id must be a UUID", status.Convert(err).Message())
			assert.False(t, accounts.called)
		})
	}
}

// Every RPC shares the mapping, so each case runs through all of them.
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
		{name: "no such user", err: account.ErrUserNotFound, want: codes.NotFound},
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
			_, getUserErr := srv.GetUser(context.Background(), &identityv1.GetUserRequest{UserId: testID.String()})

			for _, err := range []error{registerErr, loginErr, getUserErr} {
				assert.Equal(t, tt.want, status.Code(err))
				assert.Equal(t, tt.err.Error(), status.Convert(err).Message())
			}
		})
	}
}

func assertTestTokens(t *testing.T, got *identityv1.Tokens) {
	t.Helper()
	assert.Equal(t, testTokens.Access.Value, got.GetAccessToken().GetToken())
	assert.Equal(t, testTokens.Access.ExpiresAt, got.GetAccessToken().GetExpiresAt().AsTime())
	assert.Equal(t, testTokens.Refresh.Value, got.GetRefreshToken().GetToken())
	assert.Equal(t, testTokens.Refresh.ExpiresAt, got.GetRefreshToken().GetExpiresAt().AsTime())
}
