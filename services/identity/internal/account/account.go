// Package account registers users, and starts, refreshes and ends their sessions. It
// knows nothing about gRPC.
package account

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/accesstoken"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/db"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/password"
	"github.com/francisco3ferraz/ride-sharing/services/identity/internal/refreshtoken"
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

type Token struct {
	Value     string
	ExpiresAt time.Time
}

// Tokens are secrets: never log them, or put them in an error.
type Tokens struct {
	Access  Token
	Refresh Token
}

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	// For every rejected refresh token alike, so the client can't tell when reuse
	// detection fired.
	ErrInvalidRefreshToken = errors.New("refresh token is invalid or expired")
)

const uniqueViolation = "23505"

// DB is what Service needs from *pgxpool.Pool: single queries, and transactions.
type DB interface {
	db.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db      DB
	queries *db.Queries
	hasher  *password.Hasher
	signer  *accesstoken.Signer
}

func New(database DB, hasher *password.Hasher, signer *accesstoken.Signer) *Service {
	return &Service{db: database, queries: db.New(database), hasher: hasher, signer: signer}
}

func (s *Service) Register(ctx context.Context, email, pw string, role Role) (User, Tokens, error) {
	// Hashing can wait seconds for a slot, so it happens before a transaction holds a
	// pool connection, here and in Login.
	hash, err := s.hasher.Hash(ctx, pw)
	if err != nil {
		// Unwrapped: a password rule error's message is shown to the user.
		return User{}, Tokens{}, err
	}

	user := User{Email: email, Role: role}
	var refresh Token
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		user.ID, err = q.CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: hash, Role: string(role)})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "users_email_key" {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		refresh, err = startSession(ctx, q, user.ID)
		return err
	})
	if err != nil {
		return User{}, Tokens{}, err
	}

	tokens, err := s.issue(user.ID, user.Role, refresh)
	if err != nil {
		return User{}, Tokens{}, err
	}
	return user, tokens, nil
}

func (s *Service) Login(ctx context.Context, email, pw string) (User, Tokens, error) {
	row, err := s.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.hasher.VerifyDummy(ctx, pw); err != nil {
			return User{}, Tokens{}, fmt.Errorf("verify password: %w", err)
		}
		return User{}, Tokens{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, Tokens{}, fmt.Errorf("get user: %w", err)
	}

	err = s.hasher.Verify(ctx, pw, row.PasswordHash)
	if errors.Is(err, password.ErrMismatch) {
		return User{}, Tokens{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, Tokens{}, fmt.Errorf("verify password: %w", err)
	}

	user := User{ID: row.ID, Email: row.Email, Role: Role(row.Role)}
	var refresh Token
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		refresh, err = startSession(ctx, q, user.ID)
		return err
	})
	if err != nil {
		return User{}, Tokens{}, err
	}

	tokens, err := s.issue(user.ID, user.Role, refresh)
	if err != nil {
		return User{}, Tokens{}, err
	}
	return user, tokens, nil
}

// Refresh marks the refresh token used and returns new tokens for the same session.
func (s *Service) Refresh(ctx context.Context, token string) (Tokens, error) {
	hash := refreshtoken.Hash(token)

	var row db.GetRefreshTokenForUpdateRow
	var refresh Token
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		row, err = q.GetRefreshTokenForUpdate(ctx, hash[:])
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}

		switch {
		case row.Revoked:
			return ErrInvalidRefreshToken
		// Before the expiry check: an expired token can belong to a session that's
		// still live, and its reuse means someone else holds that session.
		case row.Used:
			// No error yet: it would roll the revocation back.
			if err := q.RevokeSession(ctx, row.SessionID); err != nil {
				return fmt.Errorf("revoke session: %w", err)
			}
			return nil
		case row.Expired:
			return ErrInvalidRefreshToken
		}

		if err := q.MarkRefreshTokenUsed(ctx, hash[:]); err != nil {
			return fmt.Errorf("mark refresh token used: %w", err)
		}
		refresh, err = newRefreshToken(ctx, q, row.SessionID)
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	if row.Used {
		log.Printf("refresh token reused: revoked session %s of user %s", row.SessionID, row.UserID)
		return Tokens{}, ErrInvalidRefreshToken
	}

	return s.issue(row.UserID, Role(row.Role), refresh)
}

// Logout revokes the session the refresh token belongs to. An unknown token, or one
// whose session is already revoked, isn't an error: the user ends up logged out anyway.
func (s *Service) Logout(ctx context.Context, token string) error {
	hash := refreshtoken.Hash(token)
	if err := s.queries.RevokeSessionByRefreshToken(ctx, hash[:]); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (User, error) {
	row, err := s.queries.GetUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}

	return User{ID: row.ID, Email: row.Email, Role: Role(row.Role)}, nil
}

// inTx runs fn in a transaction. fn must make every query through q: s.queries runs on
// another connection, outside the transaction.
func (s *Service) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return fn(s.queries.WithTx(tx))
	})
}

// q must be bound to a transaction, so that a failure leaves no session without a token.
func startSession(ctx context.Context, q *db.Queries, userID uuid.UUID) (Token, error) {
	sessionID, err := q.CreateSession(ctx, userID)
	if err != nil {
		return Token{}, fmt.Errorf("create session: %w", err)
	}
	return newRefreshToken(ctx, q, sessionID)
}

// pgx.ErrNoRows would mean the session is gone or revoked, which every caller rules out
// first, so it stays a 500 rather than becoming ErrInvalidRefreshToken.
func newRefreshToken(ctx context.Context, q *db.Queries, sessionID uuid.UUID) (Token, error) {
	token, hash := refreshtoken.Generate()
	expiresAt, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{TokenHash: hash[:], SessionID: sessionID})
	if err != nil {
		return Token{}, fmt.Errorf("create refresh token: %w", err)
	}
	return Token{Value: token, ExpiresAt: expiresAt}, nil
}

// issue signs the access token. It runs after the transaction commits, so no access
// token exists for a user, session or rotation that was rolled back.
func (s *Service) issue(userID uuid.UUID, role Role, refresh Token) (Tokens, error) {
	access, expiresAt, err := s.signer.Sign(userID, string(role))
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{Access: Token{Value: access, ExpiresAt: expiresAt}, Refresh: refresh}, nil
}
