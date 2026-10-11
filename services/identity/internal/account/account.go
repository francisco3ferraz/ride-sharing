// Package account registers users and logs them in. It knows nothing about gRPC.
package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/db"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
)

// Role's values are the ones stored in users.role.
type Role string

const (
	RoleRider  Role = "rider"
	RoleDriver Role = "driver"
)

type User struct {
	ID    uuid.UUID
	Email string
	Role  Role
}

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

const uniqueViolation = "23505"

type Service struct {
	queries *db.Queries
	hasher  *password.Hasher
}

func New(queries *db.Queries, hasher *password.Hasher) *Service {
	return &Service{queries: queries, hasher: hasher}
}

func (s *Service) Register(ctx context.Context, email, pw string, role Role) (User, error) {
	hash, err := s.hasher.Hash(ctx, pw)
	if err != nil {
		// Unwrapped: a password rule error's message is shown to the user.
		return User{}, err
	}

	id, err := s.queries.CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: hash, Role: string(role)})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "users_email_key" {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return User{ID: id, Email: email, Role: role}, nil
}

func (s *Service) Login(ctx context.Context, email, pw string) (User, error) {
	user, err := s.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.hasher.VerifyDummy(ctx, pw); err != nil {
			return User{}, fmt.Errorf("verify password: %w", err)
		}
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}

	err = s.hasher.Verify(ctx, pw, user.PasswordHash)
	if errors.Is(err, password.ErrMismatch) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("verify password: %w", err)
	}
	return User{ID: user.ID, Email: user.Email, Role: Role(user.Role)}, nil
}
