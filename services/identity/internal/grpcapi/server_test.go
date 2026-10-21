package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	identityv1 "github.com/francisco3ferraz/ride-sharing/gen/ridesharing/identity/v1"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/account"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/vehicle"
)

var testID = uuid.MustParse("0199d3a2-7c41-7b1e-9f2a-3c5d8e6f1a2b")

var testTokens = account.Tokens{
	Access:  account.Token{Value: "access-token", ExpiresAt: time.Date(2026, 10, 13, 9, 45, 0, 0, time.UTC)},
	Refresh: account.Token{Value: "refresh-token", ExpiresAt: time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)},
}

// fakeAccounts fails with err when it's set. Otherwise it returns testTokens and a user
// built from its arguments, so a test sees whether the role made it through both
// translations. gotToken is the refresh token it was given.
type fakeAccounts struct {
	err         error
	called      bool
	gotToken    string
	gotEmail    string
	gotPassword string
	gotClientIP netip.Addr
	gotName     *string
}

func (f *fakeAccounts) Register(_ context.Context, email, _ string, role account.Role) (account.User, account.Tokens, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, account.Tokens{}, f.err
	}
	return account.User{ID: testID, Email: email, Role: role}, testTokens, nil
}

func (f *fakeAccounts) Login(_ context.Context, email, _ string, clientIP netip.Addr) (account.User, account.Tokens, error) {
	f.called = true
	f.gotClientIP = clientIP
	if f.err != nil {
		return account.User{}, account.Tokens{}, f.err
	}
	return account.User{ID: testID, Email: email, Role: account.RoleDriver}, testTokens, nil
}

func (f *fakeAccounts) Refresh(_ context.Context, token string) (account.Tokens, error) {
	f.called = true
	f.gotToken = token
	if f.err != nil {
		return account.Tokens{}, f.err
	}
	return testTokens, nil
}

func (f *fakeAccounts) Logout(_ context.Context, token string) error {
	f.called = true
	f.gotToken = token
	return f.err
}

func (f *fakeAccounts) GetUser(_ context.Context, userID uuid.UUID) (account.User, error) {
	f.called = true
	if f.err != nil {
		return account.User{}, f.err
	}
	return account.User{ID: userID, Email: "ana@example.pt", Role: account.RoleRider}, nil
}

func (f *fakeAccounts) UpdateUser(_ context.Context, userID uuid.UUID, name *string) (account.User, error) {
	f.called = true
	f.gotName = name
	if f.err != nil {
		return account.User{}, f.err
	}
	return account.User{ID: userID, Name: "Ana", Email: "ana@example.pt", Role: account.RoleRider}, nil
}

func (f *fakeAccounts) RequestPasswordReset(_ context.Context, email string) error {
	f.called = true
	f.gotEmail = email
	return f.err
}

func (f *fakeAccounts) ResetPassword(_ context.Context, token, pw string) error {
	f.called = true
	f.gotToken = token
	f.gotPassword = pw
	return f.err
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
	accounts := &fakeAccounts{}
	srv := New(accounts)

	resp, err := srv.Login(fromClient("2001:db8::1"), &identityv1.LoginRequest{
		Email:    "ana@example.pt",
		Password: "a long enough password",
	})

	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddr("2001:db8::1"), accounts.gotClientIP)
	assert.Equal(t, testID.String(), resp.GetUser().GetId())
	assert.Equal(t, "ana@example.pt", resp.GetUser().GetEmail())
	assert.Equal(t, identityv1.Role_ROLE_DRIVER, resp.GetUser().GetRole())
	assertTestTokens(t, resp.GetTokens())
}

func TestLoginNeedsTheClientIP(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
	}{
		{name: "missing"},
		{name: "not an address", md: metadata.Pairs("client-ip", "unknown")},
		{name: "two addresses", md: metadata.Pairs("client-ip", "203.0.113.7", "client-ip", "203.0.113.8")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &fakeAccounts{}
			srv := New(accounts)

			_, err := srv.Login(metadata.NewIncomingContext(context.Background(), tt.md), &identityv1.LoginRequest{
				Email:    "ana@example.pt",
				Password: "a long enough password",
			})

			assert.Equal(t, codes.Internal, status.Code(err))
			assert.False(t, accounts.called, "accounts should not be called")
		})
	}
}

func TestLoginUnmapsIPv4(t *testing.T) {
	accounts := &fakeAccounts{}

	_, err := New(accounts).Login(fromClient("::ffff:203.0.113.7"), &identityv1.LoginRequest{Email: "ana@example.pt"})

	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddr("203.0.113.7"), accounts.gotClientIP)
}

func TestRefresh(t *testing.T) {
	accounts := &fakeAccounts{}
	srv := New(accounts)

	resp, err := srv.Refresh(context.Background(), &identityv1.RefreshRequest{RefreshToken: "old-refresh-token"})

	require.NoError(t, err)
	assert.Equal(t, "old-refresh-token", accounts.gotToken)
	assertTestTokens(t, resp.GetTokens())
}

func TestLogout(t *testing.T) {
	accounts := &fakeAccounts{}
	srv := New(accounts)

	resp, err := srv.Logout(context.Background(), &identityv1.LogoutRequest{RefreshToken: "refresh-token"})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "refresh-token", accounts.gotToken)
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

func TestUpdateUser(t *testing.T) {
	tests := []struct {
		name     string
		req      *identityv1.UpdateUserRequest
		wantName *string
	}{
		{
			name:     "name set",
			req:      &identityv1.UpdateUserRequest{UserId: testID.String(), Name: new("Ana Sofia")},
			wantName: new("Ana Sofia"),
		},
		{name: "name left out", req: &identityv1.UpdateUserRequest{UserId: testID.String()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &fakeAccounts{}
			srv := New(accounts)

			resp, err := srv.UpdateUser(context.Background(), tt.req)

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, accounts.gotName)
			assert.Equal(t, testID.String(), resp.GetUser().GetId())
			assert.Equal(t, "Ana", resp.GetUser().GetName())
			assert.Equal(t, "ana@example.pt", resp.GetUser().GetEmail())
			assert.Equal(t, identityv1.Role_ROLE_RIDER, resp.GetUser().GetRole())
		})
	}
}

func TestUpdateUserRejectsMalformedIDs(t *testing.T) {
	accounts := &fakeAccounts{}
	srv := New(accounts)

	_, err := srv.UpdateUser(context.Background(), &identityv1.UpdateUserRequest{UserId: "x", Name: new("Ana")})

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "user_id must be a UUID", status.Convert(err).Message())
	assert.False(t, accounts.called)
}

func TestGetVehicleCatalog(t *testing.T) {
	srv := New(&fakeAccounts{})

	resp, err := srv.GetVehicleCatalog(context.Background(), &identityv1.GetVehicleCatalogRequest{})

	require.NoError(t, err)
	require.Len(t, resp.GetMakes(), len(vehicle.Makes))
	for i, m := range vehicle.Makes {
		assert.Equal(t, m.Name, resp.GetMakes()[i].GetName())
		assert.Equal(t, m.Models, resp.GetMakes()[i].GetModels())
	}
	assert.Equal(t, vehicle.Colours, resp.GetColours())
}

func TestRequestPasswordReset(t *testing.T) {
	accounts := &fakeAccounts{}
	srv := New(accounts)

	resp, err := srv.RequestPasswordReset(context.Background(), &identityv1.RequestPasswordResetRequest{Email: "ana@example.pt"})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "ana@example.pt", accounts.gotEmail)
}

func TestResetPassword(t *testing.T) {
	accounts := &fakeAccounts{}
	srv := New(accounts)

	resp, err := srv.ResetPassword(context.Background(), &identityv1.ResetPasswordRequest{
		Token:    "reset-token",
		Password: "a brand new long password",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "reset-token", accounts.gotToken)
	assert.Equal(t, "a brand new long password", accounts.gotPassword)
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
		{
			name: "name breaks the rule",
			err:  fmt.Errorf("%w: must not be empty", account.ErrInvalidName),
			want: codes.InvalidArgument,
		},
		{name: "email taken", err: account.ErrEmailTaken, want: codes.AlreadyExists},
		{name: "invalid credentials", err: account.ErrInvalidCredentials, want: codes.Unauthenticated},
		{name: "invalid refresh token", err: account.ErrInvalidRefreshToken, want: codes.Unauthenticated},
		{name: "invalid reset token", err: account.ErrInvalidResetToken, want: codes.InvalidArgument},
		{name: "no such user", err: account.ErrUserNotFound, want: codes.NotFound},
		{name: "too many login attempts", err: account.ErrTooManyLoginAttempts, want: codes.ResourceExhausted},
		{
			name: "too busy to hash",
			err:  fmt.Errorf("verify password: %w", password.ErrBusy),
			want: codes.Unavailable,
		},
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
			_, loginErr := srv.Login(fromClient("203.0.113.7"), &identityv1.LoginRequest{
				Email:    "ana@example.pt",
				Password: "a long enough password",
			})
			_, refreshErr := srv.Refresh(context.Background(), &identityv1.RefreshRequest{RefreshToken: "refresh-token"})
			_, logoutErr := srv.Logout(context.Background(), &identityv1.LogoutRequest{RefreshToken: "refresh-token"})
			_, getUserErr := srv.GetUser(context.Background(), &identityv1.GetUserRequest{UserId: testID.String()})
			_, updateUserErr := srv.UpdateUser(context.Background(), &identityv1.UpdateUserRequest{UserId: testID.String(), Name: new("Ana")})
			_, requestResetErr := srv.RequestPasswordReset(context.Background(), &identityv1.RequestPasswordResetRequest{Email: "ana@example.pt"})
			_, resetErr := srv.ResetPassword(context.Background(), &identityv1.ResetPasswordRequest{
				Token:    "reset-token",
				Password: "a brand new long password",
			})

			for _, err := range []error{registerErr, loginErr, refreshErr, logoutErr, getUserErr, updateUserErr, requestResetErr, resetErr} {
				assert.Equal(t, tt.want, status.Code(err))
				assert.Equal(t, tt.err.Error(), status.Convert(err).Message())
			}
		})
	}
}

// fromClient is a call's context as the Edge sends it, for a client at addr.
func fromClient(addr string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("client-ip", addr))
}

func assertTestTokens(t *testing.T, got *identityv1.Tokens) {
	t.Helper()
	assert.Equal(t, testTokens.Access.Value, got.GetAccessToken().GetToken())
	assert.Equal(t, testTokens.Access.ExpiresAt, got.GetAccessToken().GetExpiresAt().AsTime())
	assert.Equal(t, testTokens.Refresh.Value, got.GetRefreshToken().GetToken())
	assert.Equal(t, testTokens.Refresh.ExpiresAt, got.GetRefreshToken().GetExpiresAt().AsTime())
}
